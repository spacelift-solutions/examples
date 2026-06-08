# Sample stacks that consume the spacelift-solutions/terraform-spacelift-stack
# module in a few different ways, to exercise the label extractor.

locals {
  common_labels = ["managed-by:terraform", "team:platform"]
}

# Literal list — fully resolvable by the static parser.
module "prod_api" {
  source       = "github.com/spacelift-solutions/terraform-spacelift-stack"
  name         = "prod-api"
  repository   = "infra"
  branch       = "main"
  project_root = "stacks/prod-api"

  labels = ["env:prod", "team:platform", "service:api"]
}

# Reference to a local — the parser reports this as unresolved.
module "staging_api" {
  source       = "github.com/spacelift-solutions/terraform-spacelift-stack"
  name         = "staging-api"
  repository   = "infra"
  branch       = "main"
  project_root = "stacks/staging-api"

  labels = local.common_labels
}

# concat() of a local and a literal — also unresolved statically.
module "worker" {
  source       = "github.com/spacelift-solutions/terraform-spacelift-stack?ref=v1.2.0"
  name         = "worker"
  repository   = "infra"
  branch       = "main"
  project_root = "stacks/worker"

  labels = concat(local.common_labels, ["env:prod", "service:worker"])
}

# A different module that should be ignored entirely.
module "vpc" {
  source = "terraform-aws-modules/vpc/aws"
  name   = "main-vpc"
}
