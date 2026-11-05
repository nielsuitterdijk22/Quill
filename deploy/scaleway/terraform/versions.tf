terraform {
  required_version = ">= 1.6"

  # State lives in the Scaleway Object Storage bucket (created by the bootstrap
  # apply with -backend=false). All backend settings are supplied via
  # -backend-config flags — see README.md and the CI workflow:
  #   terraform init \
  #     -backend-config="endpoint=https://s3.fr-par.scw.cloud" \
  #     -backend-config="bucket=<backup_bucket_name>-tfstate" \
  #     -backend-config="key=quill.tfstate" \
  #     -backend-config="region=fr-par" \
  #     -backend-config="access_key=$SCW_CI_ACCESS_KEY" \
  #     -backend-config="secret_key=$SCW_CI_SECRET_KEY" \
  #     -backend-config="skip_credentials_validation=true" \
  #     -backend-config="skip_region_validation=true" \
  #     -backend-config="skip_metadata_api_check=true" \
  #     -backend-config="skip_requesting_account_id=true"
  # State lives in the S3 bucket for real (CI + local share it via
  # -backend-config flags). It was commented out only for the very first
  # bootstrap apply (-backend=false) that created the bucket itself.
  backend "s3" {}

  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "~> 2.49"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }
}
