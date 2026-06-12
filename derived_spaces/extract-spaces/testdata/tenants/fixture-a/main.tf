# Fixture mirroring tenant-a's wiring styles. Expected spaces:
#   custom-teama-teamb  (literal labels)
#   teama               (local fed by ownership-JSON lookup)
locals {
  service_ownership = jsondecode(file("${path.root}/github-tech-org-owners.custom.spacelift.json"))
  svc_alpha_owner   = lookup(local.service_ownership, "your-org/svc-alpha", "")

  common_team_labels = ["team/${local.svc_alpha_owner}"]
}

# Wiring style 1: literal list — the only style extract_labels v1 resolved.
module "svc_payments_dev" {
  source = "../../modules/component-stack"

  name   = "fixture-svc-payments-dev"
  labels = ["team/teamA", "team/teamB"]
}

# Wiring style 2: labels via a local, fed by the ownership-JSON file lookup —
# the exact pattern that defeated the literal-only parser.
module "svc_alpha_dev" {
  source = "../../modules/component-stack"

  name   = "fixture-svc-alpha-dev"
  labels = local.common_team_labels
}
