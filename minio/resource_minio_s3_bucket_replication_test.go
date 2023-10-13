package minio

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/minio/madmin-go"
	"github.com/minio/minio-go/v7/pkg/replication"
)

func TestAccS3BucketReplication_oneway_simple(t *testing.T) {
	bucketName := acctest.RandomWithPrefix("tf-acc-test-a")
	secondBucketName := acctest.RandomWithPrefix("tf-acc-test-b")
	username := acctest.RandomWithPrefix("tf-acc-usr")

	primaryMinioEndpoint := os.Getenv("MINIO_ENDPOINT")
	secondaryMinioEndpoint := os.Getenv("SECOND_MINIO_ENDPOINT")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviders,
		CheckDestroy:      testAccCheckMinioS3BucketDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBucketReplicationConfigLocals(primaryMinioEndpoint, secondaryMinioEndpoint) +
					testAccBucketReplicationConfigBucket("my_bucket_in_a", "minio", bucketName) +
					testAccBucketReplicationConfigBucket("my_bucket_in_b", "secondminio", secondBucketName) +
					testAccBucketReplicationConfigPolicy(bucketName, secondBucketName) +
					testAccBucketReplicationConfigServiceAccount(username, 2) +
					`
resource "minio_s3_bucket_replication" "replication_in_b" {
  bucket     = minio_s3_bucket.my_bucket_in_a.bucket

  rule {
    delete_replication = true
    delete_marker_replication = true
    existing_object_replication = true
    metadata_sync = false

    target {
        bucket = minio_s3_bucket.my_bucket_in_b.bucket
        host = local.second_minio_host
        secure = false
        bandwidth_limt = "100M"
        access_key = minio_iam_service_account.replication_in_b.access_key
        secret_key = minio_iam_service_account.replication_in_b.secret_key
    }
  }

  depends_on = [
    minio_s3_bucket_versioning.my_bucket_in_a,
    minio_s3_bucket_versioning.my_bucket_in_b
  ]
}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckBucketHasReplication(
						"minio_s3_bucket_replication.replication_in_b",
						[]S3MinioBucketReplicationRule{
							{
								Enabled:  true,
								Priority: 1,

								Prefix: "",
								Tags:   map[string]string{},

								DeleteReplication:         true,
								DeleteMarkerReplication:   true,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            secondBucketName,
									StorageClass:      "",
									Host:              secondaryMinioEndpoint,
									Path:              "/",
									Region:            "",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    100000000,
								},
							},
						},
					),
				),
			},
			{
				ResourceName:      "minio_s3_bucket_replication.replication_in_b",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"rule.0.target.0.secret_key",
					"rule.0.priority", // This is ommited in our test case, so it gets automatically generated and thus mismatch
				},
				Config: `
resource "minio_s3_bucket_replication" "replication_in_b" {
  bucket     = minio_s3_bucket.my_bucket_in_a.bucket

  rule {
    delete_replication = true
    delete_marker_replication = true
    existing_object_replication = true
    metadata_sync = false

    target {
        bucket = minio_s3_bucket.my_bucket_in_b.bucket
        host = local.second_minio_host
        secure = false
        bandwidth_limt = "100M"
        access_key = minio_iam_service_account.replication_in_b.access_key
        # We omit the secret_key to prevent changes
    }
  }

  depends_on = [
    minio_s3_bucket_versioning.my_bucket_in_a,
    minio_s3_bucket_versioning.my_bucket_in_b
  ]
}`,
			},
		},
	})
}
func TestAccS3BucketReplication_oneway_complex(t *testing.T) {
	bucketName := acctest.RandomWithPrefix("tf-acc-test-a")
	secondBucketName := acctest.RandomWithPrefix("tf-acc-test-b")
	thirdBucketName := acctest.RandomWithPrefix("tf-acc-test-c")
	fourthBucketName := acctest.RandomWithPrefix("tf-acc-test-d")
	username := acctest.RandomWithPrefix("tf-acc-usr")

	primaryMinioEndpoint := os.Getenv("MINIO_ENDPOINT")
	secondaryMinioEndpoint := os.Getenv("SECOND_MINIO_ENDPOINT")
	thirdMinioEndpoint := os.Getenv("THIRD_MINIO_ENDPOINT")
	fourthMinioEndpoint := os.Getenv("FOURTH_MINIO_ENDPOINT")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviders,
		CheckDestroy:      testAccCheckMinioS3BucketDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBucketReplicationConfigLocals(primaryMinioEndpoint, secondaryMinioEndpoint, thirdMinioEndpoint, fourthMinioEndpoint) +
					testAccBucketReplicationConfigBucket("my_bucket_in_a", "minio", bucketName) +
					testAccBucketReplicationConfigBucket("my_bucket_in_b", "secondminio", secondBucketName) +
					testAccBucketReplicationConfigBucket("my_bucket_in_c", "thirdminio", thirdBucketName) +
					testAccBucketReplicationConfigBucket("my_bucket_in_d", "fourthminio", fourthBucketName) +
					testAccBucketReplicationConfigPolicy(bucketName, secondBucketName, thirdBucketName, fourthBucketName) +
					testAccBucketReplicationConfigServiceAccount(username, 4) +
					`
resource "minio_s3_bucket_replication" "replication_in_all" {
  bucket     = minio_s3_bucket.my_bucket_in_a.bucket

  rule {
    enabled = false

    delete_replication = true
    delete_marker_replication = false
    existing_object_replication = false
    metadata_sync = false

    priority = 10
    prefix = "bar/"

    target {
        bucket = minio_s3_bucket.my_bucket_in_b.bucket
        host = local.second_minio_host
		region = "eu-west-1"
        secure = false
        access_key = minio_iam_service_account.replication_in_b.access_key
        secret_key = minio_iam_service_account.replication_in_b.secret_key
    }
  }

  rule {
    delete_replication = false
    delete_marker_replication = true
    existing_object_replication = true
    metadata_sync = false

    priority = 100
    prefix = "foo/"

    target {
        bucket = minio_s3_bucket.my_bucket_in_c.bucket
        host = local.third_minio_host
		region = "ap-south-1"
        secure = false
        access_key = minio_iam_service_account.replication_in_c.access_key
        secret_key = minio_iam_service_account.replication_in_c.secret_key
        health_check_period = "60s"
    }
  }

  rule {
    delete_replication = true
    delete_marker_replication = false
    existing_object_replication = true
    metadata_sync = false

    priority = 200
    tags = {
      "foo" = "bar"
    }

    target {
        bucket = minio_s3_bucket.my_bucket_in_d.bucket
        host = local.fourth_minio_host
		region = "us-west-2"
        secure = false
        bandwidth_limt = "1G"
        access_key = minio_iam_service_account.replication_in_d.access_key
        secret_key = minio_iam_service_account.replication_in_d.secret_key
    }
  }

  depends_on = [
    minio_s3_bucket_versioning.my_bucket_in_a,
    minio_s3_bucket_versioning.my_bucket_in_b,
    minio_s3_bucket_versioning.my_bucket_in_c,
    minio_s3_bucket_versioning.my_bucket_in_d,
  ]
}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckBucketHasReplication(
						"minio_s3_bucket_replication.replication_in_all",
						[]S3MinioBucketReplicationRule{
							{
								Enabled:  false,
								Priority: 10,

								Prefix: "bar/",
								Tags:   map[string]string{},

								DeleteReplication:         true,
								DeleteMarkerReplication:   false,
								ExistingObjectReplication: false,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            secondBucketName,
									StorageClass:      "",
									Host:              secondaryMinioEndpoint,
									Path:              "/",
									Region:            "eu-west-1",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    0,
								},
							},
							{
								Enabled:  true,
								Priority: 100,

								Prefix: "foo/",
								Tags:   map[string]string{},

								DeleteReplication:         false,
								DeleteMarkerReplication:   true,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            thirdBucketName,
									StorageClass:      "",
									Host:              thirdMinioEndpoint,
									Path:              "/",
									Region:            "ap-south-1",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 60,
									BandwidthLimit:    0,
								},
							},
							{
								Enabled:  true,
								Priority: 200,

								Prefix: "",
								Tags: map[string]string{
									"foo": "bar",
								},

								DeleteReplication:         true,
								DeleteMarkerReplication:   false,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            fourthBucketName,
									StorageClass:      "",
									Host:              fourthMinioEndpoint,
									Path:              "/",
									Region:            "us-west-2",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    1 * humanize.BigGByte.Int64(),
								},
							},
						},
					),
				),
			},
			{
				ResourceName:      "minio_s3_bucket_replication.replication_in_all",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"rule.0.target.0.secret_key",
					"rule.1.target.0.secret_key",
					"rule.2.target.0.secret_key",
				},
				Config: `
resource "minio_s3_bucket_replication" "replication_in_all" {
bucket     = minio_s3_bucket.my_bucket_in_a.bucket

rule {
  enabled = false

  delete_replication = true
  delete_marker_replication = false
  existing_object_replication = false
  metadata_sync = false

  priority = 10
  prefix = "bar/"

  target {
      bucket = minio_s3_bucket.my_bucket_in_b.bucket
      host = local.second_minio_host
	  region = "eu-west-1"
      secure = false
      access_key = minio_iam_service_account.replication_in_b.access_key
      secret_key = minio_iam_service_account.replication_in_b.secret_key
  }
}

rule {
  delete_replication = false
  delete_marker_replication = true
  existing_object_replication = true
  metadata_sync = false

  priority = 100
  prefix = "foo/"

  target {
      bucket = minio_s3_bucket.my_bucket_in_c.bucket
      host = local.third_minio_host
	  region = "ap-south-1"
      secure = false
      access_key = minio_iam_service_account.replication_in_b.access_key
      secret_key = minio_iam_service_account.replication_in_b.secret_key
  }
}

rule {
  delete_replication = true
  delete_marker_replication = true
  existing_object_replication = true
  metadata_sync = false

  priority = 200
  tags = {
    "foo" = "bar"
  }

  target {
      bucket = minio_s3_bucket.my_bucket_in_d.bucket
      host = local.fourth_minio_host
	  region = "us-west-2"
      secure = false
      bandwidth_limt = "100M"
      access_key = minio_iam_service_account.replication_in_b.access_key
      secret_key = minio_iam_service_account.replication_in_b.secret_key
  }
}

depends_on = [
  minio_s3_bucket_versioning.my_bucket_in_a,
  minio_s3_bucket_versioning.my_bucket_in_b,
  minio_s3_bucket_versioning.my_bucket_in_c,
  minio_s3_bucket_versioning.my_bucket_in_d,
]
}`,
			},
		},
	})
}

func TestAccS3BucketReplication_twoway_simple(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-test")
	primaryMinioEndpoint := os.Getenv("MINIO_ENDPOINT")
	secondaryMinioEndpoint := os.Getenv("SECOND_MINIO_ENDPOINT")
	username := acctest.RandomWithPrefix("tf-acc-usr")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviders,
		CheckDestroy:      testAccCheckMinioS3BucketDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBucketReplicationConfigServiceAccount(username, 2) +
					testAccBucketReplicationConfigLocals(primaryMinioEndpoint, secondaryMinioEndpoint) +
					`resource "minio_s3_bucket_replication" "replication_in_b" {
              bucket     = minio_s3_bucket.my_bucket_in_a.bucket
              
              rule {
                delete_replication = true
                delete_marker_replication = true
                existing_object_replication = true
                metadata_sync = false
            
                target {
                  bucket = minio_s3_bucket.my_bucket_in_b.bucket
                  host = local.second_minio_host
                  secure = false
                  bandwidth_limt = "100M"
                  access_key = minio_iam_service_account.replication_in_b.access_key
                  secret_key = minio_iam_service_account.replication_in_b.secret_key
                }
              }
              
              depends_on = [
                minio_s3_bucket_versioning.my_bucket_in_a,
                minio_s3_bucket_versioning.my_bucket_in_b
              ]
            }

            resource "minio_s3_bucket_replication" "replication_in_a" {
              bucket     = local.bucket_name
              provider = secondminio
            
              rule {
                delete_replication = true
                delete_marker_replication = true
                existing_object_replication = true
                metadata_sync = true
            
                target {
                  bucket = local.bucket_name
                  host = local.primary_minio_host
                  secure = false
                  bandwidth_limt = "100M"
                  access_key = minio_iam_service_account.replication_in_a.access_key
                  secret_key = minio_iam_service_account.replication_in_a.secret_key
                }
              }
            }`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckBucketHasReplication(
						"minio_s3_bucket_replication.replication_in_b",
						[]S3MinioBucketReplicationRule{
							{
								Enabled:  true,
								Priority: 1,

								Prefix: "",
								Tags:   map[string]string{},

								DeleteReplication:         true,
								DeleteMarkerReplication:   true,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            name,
									StorageClass:      "",
									Host:              secondaryMinioEndpoint,
									Path:              "/",
									Region:            "eu-west-1",
									AccessKey:         "minio123",
									SecretKey:         "minio321",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    0,
								},
							},
						},
					),
					testAccCheckBucketHasReplication(
						"minio_s3_bucket_replication.replication_in_a",
						[]S3MinioBucketReplicationRule{
							{
								Enabled:  true,
								Priority: 1,

								Prefix: "",
								Tags:   map[string]string{},

								DeleteReplication:         true,
								DeleteMarkerReplication:   true,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            name,
									StorageClass:      "",
									Host:              primaryMinioEndpoint,
									Path:              "/",
									Region:            "eu-west-1",
									AccessKey:         "minio123",
									SecretKey:         "minio321",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    0,
								},
							},
						},
					),
				),
			},
			{
				ResourceName:      "minio_s3_bucket_replication.replication_in_b",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
func TestAccS3BucketReplication_twoway_complex(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-test")
	primaryMinioEndpoint := os.Getenv("MINIO_ENDPOINT")
	secondaryMinioEndpoint := os.Getenv("SECOND_MINIO_ENDPOINT")
	username := acctest.RandomWithPrefix("tf-acc-usr")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviders,
		CheckDestroy:      testAccCheckMinioS3BucketDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccBucketReplicationConfigServiceAccount(username, 2) +
					testAccBucketReplicationConfigLocals(primaryMinioEndpoint, secondaryMinioEndpoint) +
					`resource "minio_s3_bucket_replication" "replication_in_b" {
                       bucket     = minio_s3_bucket.my_bucket_in_a.bucket
                     
                       rule {
                         enabled = false

                         delete_replication = true
                         delete_marker_replication = false
                         existing_object_replication = false
                         metadata_sync = false

                         priority = 10
                         prefix = "bar/"
                     
                         target {
                              bucket = minio_s3_bucket.my_bucket_in_b.bucket
                              host = local.second_minio_host
                              secure = false
                              bandwidth_limt = "10M"
                              access_key = minio_iam_service_account.replication_in_b.access_key
                              secret_key = minio_iam_service_account.replication_in_b.secret_key
                         }
                       }
                     
                       rule {
                         delete_replication = false
                         delete_marker_replication = true
                         existing_object_replication = true
                         metadata_sync = false

                         priority = 100
                         prefix = "foo/"
                     
                         target {
                              bucket = minio_s3_bucket.my_second_bucket_in_b.bucket
                              host = local.second_minio_host
                              secure = false
                              bandwidth_limt = "10M"
                              access_key = minio_iam_service_account.replication_in_b.access_key
                              secret_key = minio_iam_service_account.replication_in_b.secret_key
                        }
                     
                       rule {
                         delete_replication = true
                         delete_marker_replication = true
                         existing_object_replication = true
                         metadata_sync = false

                         priority = 200
                         tags = {
                            "foo" = "bar"
                         }
                     
                         target {
                              bucket = minio_s3_bucket.my_third_bucket_in_b.bucket
                              host = local.second_minio_host
                              secure = false
                              bandwidth_limt = "80M"
                              access_key = minio_iam_service_account.replication_in_b.access_key
                              secret_key = minio_iam_service_account.replication_in_b.secret_key
                        }
                       }
                     
                       depends_on = [
                         minio_s3_bucket_versioning.my_bucket_in_a,
                         minio_s3_bucket_versioning.my_bucket_in_b,
                         minio_s3_bucket_versioning.my_second_bucket_in_b,
                         minio_s3_bucket_versioning.my_third_bucket_in_b,
                       ]
                      }`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckBucketHasReplication(
						"minio_s3_bucket_replication.replication_in_b",
						[]S3MinioBucketReplicationRule{
							{
								Enabled:  true,
								Priority: 1,

								Prefix: "",
								Tags:   map[string]string{},

								DeleteReplication:         true,
								DeleteMarkerReplication:   true,
								ExistingObjectReplication: true,
								MetadataSync:              false,

								Target: S3MinioBucketReplicationRuleTarget{
									Bucket:            name,
									StorageClass:      "",
									Host:              secondaryMinioEndpoint,
									Path:              "/",
									Region:            "eu-west-1",
									AccessKey:         "minio123",
									SecretKey:         "minio321",
									Syncronous:        false,
									Secure:            false,
									PathStyle:         S3PathSyleAuto,
									HealthCheckPeriod: time.Second * 30,
									BandwidthLimit:    0,
								},
							},
						},
					),
				),
			},
			{
				ResourceName:      "minio_s3_bucket_replication.replication_in_b",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// func TestAccS3BucketReplication_update(t *testing.T) {
// 	name := acctest.RandomWithPrefix("tf-acc-test")

// 	resource.ParallelTest(t, resource.TestCase{
// 		PreCheck:          func() { testAccPreCheck(t) },
// 		ProviderFactories: testAccProviders,
// 		CheckDestroy:      testAccCheckMinioS3BucketDestroy,
// 		Steps: []resource.TestStep{
// 			{
// 				Config: testAccBucketReplicationConfigLocals(name, "Enabled", []string{}, false),
// 				Check: resource.ComposeTestCheckFunc(
// 					testAccCheckMinioS3BucketExists("minio_s3_bucket.bucket"),
// 					testAccCheckBucketHasReplication(
// 						"minio_s3_bucket_replication.bucket",
// 						S3MinioBucketReplicationConfiguration{
// 							Status:           "Enabled",
// 							ExcludedPrefixes: []string{},
// 							ExcludeFolders:   false,
// 						},
// 					),
// 				),
// 			},
// 			{
// 				Config: testAccBucketReplicationConfigLocals(name, "Suspended", []string{}, false),
// 				Check: resource.ComposeTestCheckFunc(
// 					testAccCheckMinioS3BucketExists("minio_s3_bucket.bucket"),
// 					testAccCheckBucketHasReplication(
// 						"minio_s3_bucket_replication.bucket",
// 						S3MinioBucketReplicationConfiguration{
// 							Status:           "Suspended",
// 							ExcludedPrefixes: []string{},
// 							ExcludeFolders:   false,
// 						},
// 					),
// 				),
// 			},
// 			{
// 				ResourceName:      "minio_s3_bucket_replication.bucket",
// 				ImportState:       true,
// 				ImportStateVerify: true,
// 			},
// 		},
// 	})
// }

var kMinioHostIdentifier = []string{
	"primary",
	"second",
	"third",
	"fourth",
}

var kMinioHostLetter = []string{
	"a",
	"b",
	"c",
	"d",
}

func testAccBucketReplicationConfigLocals(minioHost ...string) string {
	var varBlock string
	for i, val := range minioHost {
		varBlock = varBlock + fmt.Sprintf("	%s_minio_host = %q\n", kMinioHostIdentifier[i], val)
	}
	return fmt.Sprintf(`
locals {
  %s
}
`, varBlock)
}

func testAccBucketReplicationConfigBucket(resourceName string, provider string, bucketName string) string {
	return fmt.Sprintf(`
resource "minio_s3_bucket" %q {
  provider = %s
  bucket = %q
}

resource "minio_s3_bucket_versioning" %q {
  provider = %s
  bucket     = %q

  versioning_configuration {
    status = "Enabled"
  }

  depends_on = [
    minio_s3_bucket.%s
  ]
}
`, resourceName, provider, bucketName, resourceName, provider, bucketName, resourceName)
}

func testAccBucketReplicationConfigServiceAccount(username string, count int) (varBlock string) {
	for i := 0; i < count; i++ {
		indentifier := kMinioHostIdentifier[i]
		if i == 0 {
			indentifier = "minio"
		} else {
			indentifier = indentifier + "minio"
		}
		letter := kMinioHostLetter[i]
		varBlock = varBlock + fmt.Sprintf(`
resource "minio_iam_policy" "replication_in_%s" {
  provider = %s
  name   = "ReplicationToMyBucketPolicy"
  policy = data.minio_iam_policy_document.replication_policy.json
}

resource "minio_iam_user" "replication_in_%s" {
  provider = %s
  name = %q
  force_destroy = true
} 

resource "minio_iam_user_policy_attachment" "replication_in_%s" {
  provider = %s
  user_name   = minio_iam_user.replication_in_%s.name
  policy_name = minio_iam_policy.replication_in_%s.id
}

resource "minio_iam_service_account" "replication_in_%s" {
  provider = %s
  target_user = minio_iam_user.replication_in_%s.name
}

`, letter, indentifier, letter, indentifier, username, letter, indentifier, letter, letter, letter, indentifier, letter)
	}
	return varBlock
}

func testAccBucketReplicationConfigPolicy(bucketArn ...string) string {
	bucketObjectArn := make([]string, len(bucketArn))
	for i, bucket := range bucketArn {
		bucketArn[i] = fmt.Sprintf("\"arn:aws:s3:::%s\"", bucket)
		bucketObjectArn[i] = fmt.Sprintf("\"arn:aws:s3:::%s/*\"", bucket)
	}
	return fmt.Sprintf(`
data "minio_iam_policy_document" "replication_policy" {
  statement {
    sid       = "ReadBuckets"
    effect    = "Allow"
    resources = ["arn:aws:s3:::*"]

    actions = [
      "s3:ListBucket",
    ]
  }

  statement {
    sid       = "EnableReplicationOnBucket"
    effect    = "Allow"
    resources = [%s]

    actions = [
      "s3:GetReplicationConfiguration",
      "s3:ListBucket",
      "s3:ListBucketMultipartUploads",
      "s3:GetBucketLocation",
      "s3:GetBucketVersioning",
      "s3:GetBucketObjectLockConfiguration",
      "s3:GetEncryptionConfiguration",
    ]
  }

  statement {
    sid       = "EnableReplicatingDataIntoBucket"
    effect    = "Allow"
    resources = [%s]

    actions = [
      "s3:GetReplicationConfiguration",
      "s3:ReplicateTags",
      "s3:AbortMultipartUpload",
      "s3:GetObject",
      "s3:GetObjectVersion",
      "s3:GetObjectVersionTagging",
      "s3:PutObject",
      "s3:PutObjectRetention",
      "s3:PutBucketObjectLockConfiguration",
      "s3:PutObjectLegalHold",
      "s3:DeleteObject",
      "s3:ReplicateObject",
      "s3:ReplicateDelete",
    ]
  }
}
`, strings.Join(bucketArn, ","), strings.Join(bucketObjectArn, ","))
}

func testAccCheckBucketHasReplication(n string, config []S3MinioBucketReplicationRule) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no ID is set")
		}

		minioC := testAccProvider.Meta().(*S3MinioClient).S3Client
		minioadm := testAccProvider.Meta().(*S3MinioClient).S3Admin
		actualConfig, err := minioC.GetBucketReplication(context.Background(), rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error on GetBucketReplication: %v", err)
		}

		if len(actualConfig.Rules) != len(config) {
			return fmt.Errorf("non-equivalent status error:\n\nexpected: %d\n\ngot: %d", len(actualConfig.Rules), len(config))
		}

		// Check computed fields
		// for i, rule := range config {
		// 	if id, ok := rs.Primary.Attributes[fmt.Sprintf("rule.%d.id", i)]; !ok || len(id) != 20 {
		// 		return fmt.Errorf("Rule#%d doesn't have a valid ID: %q", i, id)
		// 	}
		// 	if arn, ok := rs.Primary.Attributes[fmt.Sprintf("rule.%d.arn", i)]; !ok || len(arn) != len(fmt.Sprintf("arn:minio:replication::xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:%s", rule.Target.Bucket)) {
		// 		return fmt.Errorf("Rule#%d doesn't have a valid ARN:\n\nexpected: arn:minio:replication::xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:%s\n\ngot: %v", i, rule.Target.Bucket, arn)
		// 	}
		// }

		// Check bucket replication
		actualReplicationConfigByPriority := map[int]replication.Rule{}
		for _, rule := range actualConfig.Rules {
			actualReplicationConfigByPriority[rule.Priority] = rule
		}
		for _, rule := range config {
			existingRule, ok := actualReplicationConfigByPriority[rule.Priority]
			if !ok {
				return fmt.Errorf("Rule with priority %d not found. Available: %v", rule.Priority, actualReplicationConfigByPriority)
			}
			if (existingRule.Status == replication.Enabled) != rule.Enabled {
				return fmt.Errorf("Mismatch status:\n\nexpected: %v\n\ngot: %v", (existingRule.Status == replication.Enabled), rule.Enabled)
			}
			if existingRule.Priority != rule.Priority {
				return fmt.Errorf("Mismatch priority:\n\nexpected: %v\n\ngot: %v", existingRule.Priority, rule.Priority)
			}
			if (existingRule.DeleteMarkerReplication.Status == replication.Enabled) != rule.DeleteMarkerReplication {
				return fmt.Errorf("Mismatch DeleteMarkerReplication:\n\nexpected: %v\n\ngot: %v", (existingRule.DeleteMarkerReplication.Status == replication.Enabled), rule.DeleteMarkerReplication)
			}
			if (existingRule.DeleteReplication.Status == replication.Enabled) != rule.DeleteReplication {
				return fmt.Errorf("Mismatch DeleteReplication:\n\nexpected: %v\n\ngot: %v", (existingRule.DeleteReplication.Status == replication.Enabled), rule.DeleteReplication)
			}
			if (existingRule.SourceSelectionCriteria.ReplicaModifications.Status == replication.Enabled) != rule.MetadataSync {
				return fmt.Errorf("Mismatch SourceSelectionCriteria:\n\nexpected: %v\n\ngot: %v", (existingRule.SourceSelectionCriteria.ReplicaModifications.Status == replication.Enabled), rule.MetadataSync)
			}
			if (existingRule.ExistingObjectReplication.Status == replication.Enabled) != rule.ExistingObjectReplication {
				return fmt.Errorf("Mismatch ExistingObjectReplication:\n\nexpected: %v\n\ngot: %v", (existingRule.ExistingObjectReplication.Status == replication.Enabled), rule.ExistingObjectReplication)
			}
			if !strings.HasPrefix(existingRule.Destination.Bucket, "arn:minio:replication::") {
				return fmt.Errorf("Mismatch ARN bucket prefix:\n\nexpected: arn:minio:replication::\n\ngot: %v", existingRule.Destination.Bucket)
			}
			if !strings.HasSuffix(existingRule.Destination.Bucket, ":"+rule.Target.Bucket) {
				return fmt.Errorf("Mismatch Target bucket name:\n\nexpected: %v\n\ngot: %v", existingRule.Destination.Bucket, rule.Target.Bucket)
			}
			if existingRule.Destination.StorageClass != rule.Target.StorageClass {
				return fmt.Errorf("Mismatch Target StorageClass:\n\nexpected: %v\n\ngot: %v", existingRule.Destination.StorageClass, rule.Target.StorageClass)
			}
			if existingRule.Prefix() != rule.Prefix {
				return fmt.Errorf("Mismatch Prefix:\n\nexpected: %v\n\ngot: %v", existingRule.Prefix(), rule.Prefix)
			}
			tags := strings.Split(existingRule.Tags(), "&")
			for i, v := range tags {
				if v != "" {
					continue
				}
				tags = append(tags[:i], tags[i+1:]...)
			}
			if len(tags) != len(rule.Tags) {
				return fmt.Errorf("Mismatch tags:\n\nexpected: %v (size %d)\n\ngot: %v (size %d)", tags, len(tags), rule.Tags, len(rule.Tags))
			}
			for _, kv := range tags {
				val := strings.SplitN(kv, "=", 2)
				k := val[0]
				v := val[1]
				if cv, ok := rule.Tags[k]; !ok || v != cv {
					return fmt.Errorf("Mismatch tags:\n\nexpected: %s=%q\n\ngot: %s=%q (found: %t)", k, v, k, cv, ok)
				}
			}

			// TODO check secret key
		}

		// Check remote target
		actualTargets, err := minioadm.ListRemoteTargets(context.Background(), rs.Primary.ID, "")
		if err != nil {
			return fmt.Errorf("error on ListRemoteTargets: %v", err)
		}

		if len(actualTargets) != len(config) {
			return fmt.Errorf("non-equivalent status error:\n\nexpected: %d\n\ngot: %d", len(actualTargets), len(config))
		}
		actualRemoteTargetByArn := map[string]madmin.BucketTarget{}
		for _, target := range actualTargets {
			actualRemoteTargetByArn[target.Arn] = target
		}
		for _, rule := range config {
			existingRule, ok := actualReplicationConfigByPriority[rule.Priority]
			if !ok {
				return fmt.Errorf("Rule with priority %d not found. Available: %v", rule.Priority, actualReplicationConfigByPriority)
			}
			existingTarget, ok := actualRemoteTargetByArn[existingRule.Destination.Bucket]
			if !ok {
				return fmt.Errorf("Target with ARN %q not found. Available: %v", existingRule.Destination.Bucket, actualRemoteTargetByArn)

			}

			if existingTarget.Endpoint != rule.Target.Host {
				return fmt.Errorf("Mismatch endpoint:\n\nexpected: %v\n\ngot: %v", existingTarget.Endpoint, rule.Target.Host)
			}
			if existingTarget.Secure != rule.Target.Secure {
				return fmt.Errorf("Mismatch Secure:\n\nexpected: %v\n\ngot: %v", existingTarget.Secure, rule.Target.Secure)
			}
			if existingTarget.BandwidthLimit != rule.Target.BandwidthLimit {
				return fmt.Errorf("Mismatch BandwidthLimit:\n\nexpected: %v\n\ngot: %v", existingTarget.BandwidthLimit, rule.Target.BandwidthLimit)
			}
			if existingTarget.HealthCheckDuration != rule.Target.HealthCheckPeriod {
				return fmt.Errorf("Mismatch HealthCheckDuration:\n\nexpected: %v\n\ngot: %v", existingTarget.HealthCheckDuration, rule.Target.HealthCheckPeriod)
			}
			if existingTarget.Secure != rule.Target.Secure {
				return fmt.Errorf("Mismatch Secure:\n\nexpected: %v\n\ngot: %v", existingTarget.Secure, rule.Target.Secure)
			}
			bucket := rule.Target.Bucket
			cleanPath := strings.TrimPrefix(strings.TrimPrefix(rule.Target.Path, "/"), ".")
			if cleanPath != "" {
				bucket = cleanPath + "/" + rule.Target.Bucket
			}
			if existingTarget.TargetBucket != bucket {
				return fmt.Errorf("Mismatch TargetBucket:\n\nexpected: %v\n\ngot: %v", existingTarget.TargetBucket, bucket)
			}
			if existingTarget.ReplicationSync != rule.Target.Syncronous {
				return fmt.Errorf("Mismatch synchronous mode:\n\nexpected: %v\n\ngot: %v", existingTarget.ReplicationSync, rule.Target.Syncronous)
			}
			if existingTarget.Region != rule.Target.Region {
				return fmt.Errorf("Mismatch region:\n\nexpected: %v\n\ngot: %v", existingTarget.Region, rule.Target.Region)
			}
			if existingTarget.Path != rule.Target.PathStyle.String() {
				return fmt.Errorf("Mismatch path style:\n\nexpected: %v\n\ngot: %v", existingTarget.Path, rule.Target.PathStyle.String())
			}
			// Asserting exact AccessKey value is too painful. Furhtermore, since MinIO assert the credential validity before accepting the new remote target, the value is very low
			if len(existingTarget.Credentials.AccessKey) != 20 {
				return fmt.Errorf("Mismatch AccessKey:\n\nexpected: 20-char string\n\ngot: %v", existingTarget.Credentials.AccessKey)
			}
		}

		return nil
	}
}
