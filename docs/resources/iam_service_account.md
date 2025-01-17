---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "minio_iam_service_account Resource - terraform-provider-minio"
subcategory: ""
description: |-
---

# minio_iam_service_account (Resource)

## Example Usage

```terraform
resource "minio_iam_user" "test" {
   name = "test"
   force_destroy = true
   tags = {
    tag-key = "tag-value"
  }
}

resource "minio_iam_service_account" "test_service_account" {
  target_user = minio_iam_user.test.name
}

output "minio_user" {
  value = minio_iam_service_account.test_service_account.access_key
}

output "minio_password" {
  value     = minio_iam_service_account.test_service_account.secret_key
  sensitive = true
}
```

<!-- schema generated by tfplugindocs -->

## Schema

### Required

- `target_user` (String)

### Optional

- `disable_user` (Boolean) Disable service account
- `update_secret` (Boolean) rotate secret key

### Read-Only

- `access_key` (String)
- `id` (String) The ID of this resource.
- `secret_key` (String, Sensitive)
- `status` (String)
