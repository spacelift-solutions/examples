# Tenant admin stack B. Continues the wiring-style fixtures from tenant-a.
locals {
  # null = use the public worker pool.
  worker_pool_id = null
  base_labels    = ["env/dev", "env/poc"]
}

# Wiring style 3: labels from a variable populated by terraform.tfvars.
# Space: custom-teama-teamb-teamc.
module "svc_data_platform_dev" {
  source = "../../modules/component-stack"

  name           = "svc-data-platform-dev"
  labels         = var.team_labels
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}

# Wiring style 4: concat() of a local and an inline list. The env/dev label
# passes through without affecting the space. Resolves to the same "teama"
# space as tenant-a's svc-alpha stack — the shared-space dedupe proof.
module "svc_search_dev" {
  source = "../../modules/component-stack"

  name           = "svc-search-dev"
  labels         = concat(local.base_labels, ["team/teamA"])
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}

# Shared new combination (validation scenario 3): tenant-a declares the same
# teamA+teamE combo in the same PR — exactly one space must be created.
module "svc_billing_etl_dev" {
  source = "../../modules/component-stack"

  name           = "svc-billing-etl-dev"
  labels         = ["team/teamA", "team/teamE"]
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}
