# Tenant admin stack A. Each module call below deliberately wires labels a
# different way teams do in the wild — every style here is also a fixture for
# the extract-spaces parser.
locals {
  # null = use the public worker pool.
  worker_pool_id = null

  # Mirrors a repo->owner mapping: the owning team comes from a JSON file
  # lookup rather than being written inline.
  service_ownership = jsondecode(file("${path.root}/github-tech-org-owners.custom.spacelift.json"))
  svc_alpha_owner   = lookup(local.service_ownership, "your-org/svc-alpha", "")

  common_team_labels = ["team/${local.svc_alpha_owner}"]
}

# Wiring style 1: literal labels — the only style the old literal-only
# extract_labels parser could resolve. Space: custom-teama-teamb.
module "svc_payments_dev" {
  source = "../../modules/component-stack"

  name           = "svc-payments-dev"
  labels         = ["team/teamA", "team/teamB"]
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}

# Wiring style 2: labels via a local fed by the ownership-JSON lookup — the
# exact pattern that defeated the literal-only parser. Space: teama.
module "svc_alpha_dev" {
  source = "../../modules/component-stack"

  name           = "svc-alpha-dev"
  labels         = local.common_team_labels
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}

# Shared new combination (validation scenario 3): tenant-b declares the same
# teamA+teamE combo in the same PR — exactly one space must be created.
module "svc_billing_dev" {
  source = "../../modules/component-stack"

  name           = "svc-billing-dev"
  labels         = ["team/teamA", "team/teamE"]
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}

# New combination under the run-type-aware precondition (scenario 8): the
# proposed run for this PR must be GREEN even though custom-teama-teamf does
# not exist yet.
module "svc_notify_dev" {
  source = "../../modules/component-stack"

  name           = "svc-notify-dev"
  labels         = ["team/teamA", "team/teamF"]
  worker_pool_id = local.worker_pool_id
  run_type       = var.spacelift_run_type
}
