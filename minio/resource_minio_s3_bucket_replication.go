package minio

import (
	"context"
	"fmt"
	"log"
	"math"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/minio/madmin-go"
	"github.com/minio/minio-go/v7/pkg/replication"
	"github.com/minio/minio-go/v7/pkg/s3utils"
	"github.com/rs/xid"
	"golang.org/x/exp/slices"
)

func resourceMinioBucketReplication() *schema.Resource {
	return &schema.Resource{
		CreateContext: minioPutBucketReplication,
		ReadContext:   minioReadBucketReplication,
		UpdateContext: minioPutBucketReplication,
		DeleteContext: minioDeleteBucketReplication,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			"bucket": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"rule": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 10, // Is there a max?
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"arn": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"enabled": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  true,
						},
						"priority": {
							Type:         schema.TypeInt,
							Optional:     true,
							ValidateFunc: validation.IntAtLeast(1),
							DiffSuppressFunc: func(k, oldValue, newValue string, d *schema.ResourceData) bool {
								oldVal, _ := strconv.Atoi(oldValue)
								newVal, _ := strconv.Atoi(newValue)

								log.Printf("[DEBUG] Priority diff: %s(%d) %s(%d) -> %t", oldValue, oldVal, newValue, newVal, oldVal < 0 && newVal == 0 || oldVal == newVal)
								return oldVal < 0 && newVal == 0 || oldVal == newVal
							},
						},
						"prefix": {
							Type:     schema.TypeString,
							Optional: true,
							Default:  "",
						},
						"tags": {
							Type:     schema.TypeMap,
							Optional: true,
							ValidateDiagFunc: validation.AllDiag(
								validation.MapValueMatch(regexp.MustCompile(`^[a-zA-Z0-9-+\-._:/@ ]+$`), ""),
								validation.MapKeyMatch(regexp.MustCompile(`^[a-zA-Z0-9-+\-._:/@ ]+$`), ""),
								validation.MapValueLenBetween(1, 256),
								validation.MapKeyLenBetween(1, 128),
							),
						},
						"delete_replication": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"delete_marker_replication": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"existing_object_replication": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"metadata_sync": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"target": {
							Type:     schema.TypeList,
							MinItems: 1,
							MaxItems: 1,
							Required: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"bucket": {
										Type:     schema.TypeString,
										Required: true,
									},
									"storage_class": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"host": {
										Type:     schema.TypeString,
										Required: true,
									},
									"secure": {
										Type:     schema.TypeBool,
										Optional: true,
										Default:  true,
									},
									"path_style": {
										Type:         schema.TypeString,
										Optional:     true,
										Default:      "auto",
										ValidateFunc: validation.StringInSlice([]string{"on", "off", "auto"}, true),
									},
									"path": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"syncronous": {
										Type:     schema.TypeBool,
										Optional: true,
										Default:  false,
									},
									"health_check_period": {
										Type:     schema.TypeString,
										Optional: true,
										Default:  "30s",
										DiffSuppressFunc: func(k, oldValue, newValue string, d *schema.ResourceData) bool {
											newVal, err := time.ParseDuration(newValue)
											return err == nil && shortDur(newVal) == oldValue
										},
										ValidateFunc: validation.StringMatch(regexp.MustCompile(`^[0-9]+\s?[s|m|h]$`), "must be a valid golang duration"),
									},
									"bandwidth_limt": {
										Type:     schema.TypeString,
										Optional: true,
										Default:  "0",
										DiffSuppressFunc: func(k, oldValue, newValue string, d *schema.ResourceData) bool {
											newVal, err := humanize.ParseBytes(newValue)
											return err == nil && humanize.Bytes(newVal) == oldValue
										},
										ValidateDiagFunc: func(i interface{}, _ cty.Path) (diags diag.Diagnostics) {
											v, ok := i.(string)
											if !ok {
												diags = append(diags, diag.Diagnostic{
													Severity: diag.Error,
													Summary:  "expected type of bandwidth_limt to be string",
												})
												return
											}

											if v == "" {
												return
											}

											val, err := humanize.ParseBytes(v)
											if err != nil {
												diags = append(diags, diag.Diagnostic{
													Severity: diag.Error,
													Summary:  "bandwidth_limt must be a positive value. It may use traditional suffixes (k, m, g, ..) ",
												})
												return
											}
											if val < uint64(100*humanize.BigMByte.Int64()) {
												diags = append(diags, diag.Diagnostic{
													Severity: diag.Error,
													Summary:  "When set, bandwidth_limt must be at least 100MBps",
												})

											}
											return
										},
									},
									"region": {
										Type:     schema.TypeString,
										Optional: true,
									},
									"access_key": {
										Type:         schema.TypeString,
										Required:     true,
										ValidateFunc: validation.StringIsNotEmpty,
									},
									"secret_key": {
										Type:         schema.TypeString,
										Optional:     true, // This is optional to allow import and then prevent credential changes
										Sensitive:    true,
										ValidateFunc: validation.StringIsNotEmpty,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func minioPutBucketReplication(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	bucketReplicationConfig, diags := BucketReplicationConfig(d, meta)
	replicationConfig := bucketReplicationConfig.ReplicationRules

	if replicationConfig == nil || diags.HasError() {
		return diags
	}

	log.Printf("[DEBUG] S3 bucket: %s, put replication configuration: %v", bucketReplicationConfig.MinioBucket, replicationConfig)

	cfg, err := convertBucketReplicationConfig(bucketReplicationConfig, replicationConfig)

	if err != nil {
		return NewResourceError(fmt.Sprintf("error generating bucket replication configuration for %q", bucketReplicationConfig.MinioBucket), d.Id(), err)
	}

	err = bucketReplicationConfig.MinioClient.SetBucketReplication(
		ctx,
		bucketReplicationConfig.MinioBucket,
		cfg,
	)

	if err != nil {
		return NewResourceError(fmt.Sprintf("error putting bucket replication configuration for %q", bucketReplicationConfig.MinioBucket), d.Id(), err)
	}

	d.SetId(bucketReplicationConfig.MinioBucket)

	return nil
}

func minioReadBucketReplication(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	bucketReplicationConfig, diags := BucketReplicationConfig(d, meta)

	if diags.HasError() {
		return diags
	}

	client := bucketReplicationConfig.MinioClient
	admclient := bucketReplicationConfig.MinioAdmin
	bucketName := d.Id()

	// Reverse index to store rule definition read from Minio to macth the order they have in the IaC. This prevent Terrfaform from try to re-order rule each time
	rulePriorityMap := map[int]int{}
	// Reverse index to store arn and index in the rule set. This is used to match bucket config and remote target order
	ruleArnMap := map[string]int{}

	if bucketReplicationConfig.ReplicationRules != nil {
		for idx, rule := range bucketReplicationConfig.ReplicationRules {
			priority := rule.Priority
			if priority == 0 {
				priority = -idx - 1
			}
			rulePriorityMap[priority] = idx
		}
	}

	log.Printf("[DEBUG] S3 bucket replication, read for bucket: %s", bucketName)

	// First, gather the bucket replication config
	rcfg, err := client.GetBucketReplication(ctx, bucketName)
	if err != nil {
		log.Printf("[WARN] Unable to fetch bucket replication config for %q: %v", bucketName, err)
		return diag.FromErr(fmt.Errorf("error reading bucket replication configuration: %s", err))
	}

	rules := make([]map[string]interface{}, len(rcfg.Rules))

	for idx, rule := range rcfg.Rules {
		var ruleIdx int
		var ok bool
		if ruleIdx, ok = rulePriorityMap[rule.Priority]; !ok {
			ruleIdx = idx
		}
		if _, ok = ruleArnMap[rule.Destination.Bucket]; ok {
			log.Printf("[WARN] Conflict detetcted between two rules containing the same ARN for %q: %q", bucketName, rule.Destination.Bucket)
			return diag.FromErr(fmt.Errorf("conflict detetcted between two rules containing the same ARN for %q: %q", bucketName, rule.Destination.Bucket))
		}
		ruleArnMap[rule.Destination.Bucket] = ruleIdx
		target := map[string]interface{}{
			"storage_class": rule.Destination.StorageClass,
		}
		priority := rule.Priority
		if len(bucketReplicationConfig.ReplicationRules) > ruleIdx && priority == -bucketReplicationConfig.ReplicationRules[ruleIdx].Priority {
			priority = -priority
		}
		rules[ruleIdx] = map[string]interface{}{
			"id":                          rule.ID,
			"arn":                         rule.Destination.Bucket,
			"enabled":                     rule.Status == replication.Enabled,
			"priority":                    priority,
			"prefix":                      rule.Prefix(),
			"delete_replication":          rule.DeleteReplication.Status == replication.Enabled,
			"delete_marker_replication":   rule.DeleteMarkerReplication.Status == replication.Enabled,
			"existing_object_replication": rule.ExistingObjectReplication.Status == replication.Enabled,
			"metadata_sync":               rule.SourceSelectionCriteria.ReplicaModifications.Status == replication.Enabled,
		}

		log.Printf("[DEBUG] Rule data for rule#%d is: %q", ruleIdx, rule)

		if len(rule.Filter.And.Tags) != 0 || rule.Filter.And.Prefix != "" {
			tags := map[string]string{}
			for _, tag := range rule.Filter.And.Tags {
				if tag.IsEmpty() {
					continue
				}
				tags[tag.Key] = tag.Value
			}
			rules[ruleIdx]["tags"] = tags
		} else if rule.Filter.Tag.Key != "" {
			rules[ruleIdx]["tags"] = map[string]string{
				rule.Filter.Tag.Key: rule.Filter.Tag.Value,
			}
		} else {
			rules[ruleIdx]["tags"] = nil
		}

		// During import, there is no rules defined. Furthermore, since it is impossible to read the secret from the API, we
		// default it to an empty string, allowing user to prevent remote changes by also using an empty string or omiting the secret_key
		if len(bucketReplicationConfig.ReplicationRules) > ruleIdx {
			target["secret_key"] = bucketReplicationConfig.ReplicationRules[ruleIdx].Target.SecretKey
		}

		rules[ruleIdx]["target"] = []interface{}{target}
	}

	// Second, we read the remote bucket config
	existingRemoteTargets, err := admclient.ListRemoteTargets(ctx, bucketName, "")
	if err != nil {
		log.Printf("[WARN] Unable to fetch existing remote target config for %q: %v", bucketName, err)
		return diag.FromErr(fmt.Errorf("error reading replication remote target configuration: %s", err))
	}

	if len(existingRemoteTargets) != len(rules) {
		return diag.FromErr(fmt.Errorf("inconsistent number of remote target and bucket replication rules (%d != %d)", len(existingRemoteTargets), len(rules)))
	}

	for _, remoteTarget := range existingRemoteTargets {
		var ruleIdx int
		var ok bool
		var target map[string]interface{}
		if ruleIdx, ok = ruleArnMap[remoteTarget.Arn]; !ok {
			return diag.FromErr(fmt.Errorf("unable to find the remote target configuration for ARN %q on %s", remoteTarget.Arn, bucketName))
		}
		var targets []interface{}
		if targets, ok = rules[ruleIdx]["target"].([]interface{}); !ok || len(targets) != 1 {
			return diag.FromErr(fmt.Errorf("unable to find the bucket replication configuration associated to ARN %q (rule#%d) on %s", remoteTarget.Arn, ruleIdx, bucketName))
		}
		if target, ok = targets[0].(map[string]interface{}); !ok || len(target) == 0 {
			return diag.FromErr(fmt.Errorf("unable to extract the target information for the this remote target configuration associated on %s", bucketName))
		}

		pathComponent := strings.Split(remoteTarget.TargetBucket, "/")

		log.Printf("[DEBUG] absolute remote target path is %s", remoteTarget.TargetBucket)

		target["bucket"] = pathComponent[len(pathComponent)-1]
		target["host"] = remoteTarget.Endpoint
		target["secure"] = remoteTarget.Secure
		target["path_style"] = remoteTarget.Path
		target["path"] = strings.Join(pathComponent[:len(pathComponent)-1], "/")
		target["syncronous"] = remoteTarget.ReplicationSync
		target["health_check_period"] = shortDur(remoteTarget.HealthCheckDuration)
		target["bandwidth_limt"] = humanize.Bytes(uint64(remoteTarget.BandwidthLimit))
		target["region"] = remoteTarget.Region
		target["access_key"] = remoteTarget.Credentials.AccessKey

		log.Printf("[DEBUG] serialise remote target data is %v", target)

		rules[ruleIdx]["target"] = []interface{}{target}
	}

	if err := d.Set("bucket", d.Id()); err != nil {
		return diag.FromErr(fmt.Errorf("error setting replication configuration: %w", err))
	}

	if err := d.Set("rule", rules); err != nil {
		return diag.FromErr(fmt.Errorf("error setting replication configuration: %w", err))
	}

	return diags
}

func minioDeleteBucketReplication(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	bucketReplicationConfig, diags := BucketReplicationConfig(d, meta)

	if len(bucketReplicationConfig.ReplicationRules) == 0 && !diags.HasError() {
		log.Printf("[DEBUG] Removing bucket replication for unversioned bucket (%s) from state", d.Id())
	} else if diags.HasError() {
		return diags
	}

	client := bucketReplicationConfig.MinioClient
	admclient := bucketReplicationConfig.MinioAdmin

	rcfg, err := client.GetBucketReplication(ctx, bucketReplicationConfig.MinioBucket)
	if err != nil {
		log.Printf("[WARN] Unable to fetch bucket replication config for %q: %v", bucketReplicationConfig.MinioBucket, err)
		return diag.FromErr(fmt.Errorf("error reading bucket replication configuration: %s", err))
	}

	log.Printf("[DEBUG] S3 bucket: %s, disabling replication", bucketReplicationConfig.MinioBucket)

	rcfg.Rules = []replication.Rule{}
	err = client.SetBucketReplication(ctx, bucketReplicationConfig.MinioBucket, rcfg)
	if err != nil {
		log.Printf("[WARN] Unable to set an empty replication config for %q: %v", bucketReplicationConfig.MinioBucket, err)
		return diag.FromErr(fmt.Errorf("error writing bucket replication configuration: %s", err))
	}

	existingRemoteTargets, err := admclient.ListRemoteTargets(ctx, bucketReplicationConfig.MinioBucket, "")
	if err != nil {
		log.Printf("[WARN] Unable to fetch existing remote target config for %q: %v", bucketReplicationConfig.MinioBucket, err)
		return diag.FromErr(fmt.Errorf("error reading replication remote target configuration: %s", err))
	}
	if len(existingRemoteTargets) != 0 {
		return diag.FromErr(fmt.Errorf("%d remote targets are still present on the bukcet while none are expected", len(existingRemoteTargets)))
	}

	return diags
}
func toEnableFlag(b bool) string {
	if b {
		return "enable"
	}
	return "disable"
}

func convertBucketReplicationConfig(bucketReplicationConfig *S3MinioBucketReplication, c []S3MinioBucketReplicationRule) (rcfg replication.Config, err error) {
	// TODO do we want to fetch the existing config?
	client := bucketReplicationConfig.MinioClient
	admclient := bucketReplicationConfig.MinioAdmin

	ctx := context.Background() // TODO global context?

	rcfg, err = client.GetBucketReplication(ctx, bucketReplicationConfig.MinioBucket)
	if err != nil {
		log.Printf("[WARN] Unable to fetch bucket replication config for %q: %v", bucketReplicationConfig.MinioBucket, err)
		return
	}

	usedARNs := make([]string, len(c))
	existingRemoteTargets, err := admclient.ListRemoteTargets(ctx, bucketReplicationConfig.MinioBucket, "")
	if err != nil {
		log.Printf("[WARN] Unable to fetch existing remote target config for %q: %v", bucketReplicationConfig.MinioBucket, err)
		return
	}

	// TODO all rules will be regenerated, potentially overriding some. We need to add some logging
	for i, rule := range c {
		err = s3utils.CheckValidBucketName(rule.Target.Bucket)
		if err != nil {
			log.Printf("[WARN] Invalid bucket name for %q: %v", rule.Target.Bucket, err)
			return
		}

		tgtBucket := rule.Target.Bucket
		if rule.Target.Path != "" {
			tgtBucket = path.Clean("./" + rule.Target.Path + "/" + tgtBucket)
		}
		log.Printf("[DEBUG] Full path to target bucket is %s", tgtBucket)

		creds := &madmin.Credentials{AccessKey: rule.Target.AccessKey, SecretKey: rule.Target.SecretKey}
		bktTarget := &madmin.BucketTarget{
			TargetBucket:        tgtBucket,
			Secure:              rule.Target.Secure,
			Credentials:         creds,
			Endpoint:            rule.Target.Host,
			Path:                rule.Target.PathStyle.String(),
			API:                 "s3v4",
			Type:                madmin.ReplicationService,
			Region:              rule.Target.Region,
			BandwidthLimit:      rule.Target.BandwidthLimit,
			ReplicationSync:     rule.Target.Syncronous,
			DisableProxy:        false, // TODO support?
			HealthCheckDuration: rule.Target.HealthCheckPeriod,
		}
		// TODO use ListRemoteTarget if r.Id is set and fetch the existing ARN if no changes are required for the target
		targets, _ := admclient.ListRemoteTargets(ctx, bucketReplicationConfig.MinioBucket, string(madmin.ReplicationService))
		log.Printf("[DEBUG] Existing remote targets %q: %v", bucketReplicationConfig.MinioBucket, targets)
		var arn string
		log.Printf("[DEBUG] Adding new remote target %v for %q", *bktTarget, bucketReplicationConfig.MinioBucket)
		arn, err = admclient.SetRemoteTarget(ctx, bucketReplicationConfig.MinioBucket, bktTarget)
		if err != nil {
			log.Printf("[WARN] Unable to configure remote target %v for %q: %v", *bktTarget, bucketReplicationConfig.MinioBucket, err)
			return
		}

		tagList := []string{}
		for k, v := range rule.Tags {
			tagList = append(tagList, fmt.Sprintf("%s=%s", k, v)) // TODO assert key content to ensure no ampersand?
		}

		opts := replication.Options{
			TagString:               strings.Join(tagList, "&"),
			IsTagSet:                len(tagList) != 0,
			StorageClass:            rule.Target.StorageClass,
			Priority:                strconv.Itoa(int(math.Abs(float64(rule.Priority)))),
			Prefix:                  rule.Prefix,
			RuleStatus:              toEnableFlag(rule.Enabled),
			ID:                      rule.Id,
			DestBucket:              arn,
			ReplicateDeleteMarkers:  toEnableFlag(rule.DeleteMarkerReplication),
			ReplicateDeletes:        toEnableFlag(rule.DeleteReplication),
			ReplicaSync:             toEnableFlag(rule.MetadataSync),
			ExistingObjectReplicate: toEnableFlag(rule.ExistingObjectReplication),
		}
		log.Printf("[DEBUG] Adding/editing replication option for rule#%d: %v", i, opts)
		if strings.TrimSpace(opts.ID) == "" {
			rule.Id = xid.New().String()
			opts.ID = rule.Id
			opts.Op = replication.AddOption
			err = rcfg.AddRule(opts)
		} else {
			opts.Op = replication.SetOption
			err = rcfg.EditRule(opts)
		}

		if err != nil {
			return
		}
		usedARNs[i] = arn
	}

	for _, existingRemoteTarget := range existingRemoteTargets {
		if !slices.Contains(usedARNs, existingRemoteTarget.Arn) {
			err = admclient.RemoveRemoteTarget(ctx, bucketReplicationConfig.MinioBucket, existingRemoteTarget.Arn)
		}

		if err != nil {
			return
		}
	}

	return
}

func getBucketReplicationConfig(v []interface{}) (result []S3MinioBucketReplicationRule, errs diag.Diagnostics) {
	if len(v) == 0 || v[0] == nil {
		return
	}

	result = make([]S3MinioBucketReplicationRule, len(v))
	for i, rule := range v {
		var ok bool
		tfMap, ok := rule.(map[string]interface{})
		if !ok {
			errs = append(errs, diag.Errorf("Unable to extra the rule %d", i)...)
			continue
		}
		log.Printf("[DEBUG] rule[%d] contains %v", i, tfMap)

		result[i].Arn, _ = tfMap["arn"].(string)
		result[i].Id, _ = tfMap["id"].(string)

		if result[i].Enabled, ok = tfMap["enabled"].(bool); !ok {
			log.Printf("[DEBUG] rule[%d].enabled omitted. Defaulting to true", i)
			result[i].Enabled = true
		}

		if result[i].Priority, ok = tfMap["priority"].(int); !ok || result[i].Priority == 0 {
			// Since priorities are always positive, we use a negative value to indicate they were automatically generated
			result[i].Priority = -i - 1
			log.Printf("[DEBUG] rule[%d].priority omitted. Defaulting to index (%d)", i, -result[i].Priority) // TODO shall we use max(rules[].priority) + 1 instead?
		}

		result[i].Prefix, _ = tfMap["prefix"].(string)

		if tags, ok := tfMap["tags"].(map[string]interface{}); ok {
			log.Printf("[DEBUG] rule[%d].tags map contains: %v", i, tags)
			tagMap := map[string]string{}
			for k, val := range tags {
				var valOk bool
				tagMap[k], valOk = val.(string)
				if !valOk {
					errs = append(errs, diag.Errorf("rule[%d].tags[%s] value must be a string, not a %s", i, k, reflect.TypeOf(val))...)
				}
			}
			result[i].Tags = tagMap
		} else {
			errs = append(errs, diag.Errorf("unable to extarct rule[%d].tags of type %s", i, reflect.TypeOf(tfMap["tags"]))...)
		}

		log.Printf("[DEBUG] rule[%d].tags are: %v", i, result[i].Tags)

		result[i].DeleteReplication, ok = tfMap["delete_replication"].(bool)
		result[i].DeleteReplication = result[i].DeleteReplication && ok
		result[i].DeleteMarkerReplication, ok = tfMap["delete_marker_replication"].(bool)
		result[i].DeleteMarkerReplication = result[i].DeleteMarkerReplication && ok
		result[i].ExistingObjectReplication, ok = tfMap["existing_object_replication"].(bool)
		result[i].ExistingObjectReplication = result[i].ExistingObjectReplication && ok

		// TODO target
		var targets []interface{}
		if targets, ok = tfMap["target"].([]interface{}); !ok || len(targets) != 1 {
			errs = append(errs, diag.Errorf("Unexpected value type for rule[%d].target. Exactly one target configuration is expected", i)...)
			continue
		}
		var target map[string]interface{}
		if target, ok = targets[0].(map[string]interface{}); !ok {
			errs = append(errs, diag.Errorf("Unexpected value type for rule[%d].target. Unable to convert to a usable type", i)...)
			continue
		}

		if result[i].Target.Bucket, ok = target["bucket"].(string); !ok {
			errs = append(errs, diag.Errorf("rule[%d].target.bucket cannot be omitted", i)...)
		}

		result[i].Target.StorageClass, _ = target["storage_class"].(string)

		if result[i].Target.Host, ok = target["host"].(string); !ok {
			errs = append(errs, diag.Errorf("rule[%d].target.host cannot be omitted", i)...)
		}

		result[i].Target.Path, _ = target["path"].(string)
		result[i].Target.Region, _ = target["region"].(string)

		if result[i].Target.AccessKey, ok = target["access_key"].(string); !ok {
			errs = append(errs, diag.Errorf("rule[%d].target.access_key cannot be omitted", i)...)
		}

		if result[i].Target.SecretKey, ok = target["secret_key"].(string); !ok {
			errs = append(errs, diag.Errorf("rule[%d].target.secret_key cannot be omitted", i)...)
		}

		if result[i].Target.Secure, ok = target["secure"].(bool); !ok {
			// TODO shall we add a envvar to silent this warning?
			errs = append(errs, diag.Diagnostic{
				Severity: diag.Warning,
				Summary:  fmt.Sprintf("rule[%d].target.secure is false. It is unsafe to use bucket replication over HTTP", i),
			})
		}

		result[i].Target.Syncronous, ok = target["syncronous"].(bool)
		result[i].Target.Syncronous = result[i].Target.Syncronous && ok

		var bandwidthStr string
		var bandwidth uint64
		var err error
		if bandwidthStr, ok = target["bandwidth_limt"].(string); ok {
			bandwidth, err = humanize.ParseBytes(bandwidthStr)
			if err != nil {
				log.Printf("[WARN] invalid bandwidth value %q: %v", result[i].Target.BandwidthLimit, err)
				errs = append(errs, diag.Errorf("rule[%d].target.bandwidth_limt is invalid. Make sure to use k, m, g as preffix only", i)...)
			} else {
				result[i].Target.BandwidthLimit = int64(bandwidth)
			}
		}

		var healthcheckDuration string
		if healthcheckDuration, ok = target["health_check_period"].(string); ok {
			result[i].Target.HealthCheckPeriod, err = time.ParseDuration(healthcheckDuration)
			if err != nil {
				log.Printf("[WARN] invalid healthcheck value %q: %v", result[i].Target.HealthCheckPeriod, err)
				errs = append(errs, diag.Errorf("rule[%d].target.health_check_period is invalid. Make sure to use a valid golang time duration notation", i)...)
			}
		}

		var pathstyle string
		pathstyle, _ = target["path_style"].(string)
		switch strings.TrimSpace(strings.ToLower(pathstyle)) {
		case "on":
			result[i].Target.PathStyle = S3PathSyleOn
		case "off":
			result[i].Target.PathStyle = S3PathSyleOff
		default:
			if pathstyle != "auto" && pathstyle != "" {
				errs = append(errs, diag.Diagnostic{
					Severity: diag.Warning,
					Summary:  fmt.Sprintf("rule[%d].target.path_style must be \"on\", \"off\" or \"auto\". Defaulting to \"auto\"", i),
				})
			}
			result[i].Target.PathStyle = S3PathSyleAuto
		}

	}
	return
}
