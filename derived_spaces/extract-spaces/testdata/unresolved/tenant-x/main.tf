# Negative fixture: labels reference a variable with no default and no tfvars,
# so resolution must fail loudly instead of being skipped.
variable "mystery_labels" {
  type = list(string)
}

module "svc_mystery" {
  source = "../../modules/component-stack"

  name   = "fixture-svc-mystery"
  labels = var.mystery_labels
}
