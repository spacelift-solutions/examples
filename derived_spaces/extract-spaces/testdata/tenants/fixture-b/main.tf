# Fixture mirroring tenant-b's wiring styles. Expected spaces:
#   custom-teama-teamb-teamc  (var populated via terraform.tfvars)
#   teama                     (concat of local + inline, deduped with fixture-a)
locals {
  base_labels = ["env/dev"]
}

variable "team_labels" {
  type        = list(string)
  description = "Ownership labels injected via terraform.tfvars."
}

# Wiring style 3: labels from a variable populated by terraform.tfvars.
module "svc_data_platform_dev" {
  source = "../../modules/component-stack"

  name   = "fixture-svc-data-platform-dev"
  labels = var.team_labels
}

# Wiring style 4: concat() of a local and an inline list; the non-team
# env/dev label must pass through without affecting the space.
module "svc_search_dev" {
  source = "../../modules/component-stack"

  name   = "fixture-svc-search-dev"
  labels = concat(local.base_labels, ["team/teamA"])
}
