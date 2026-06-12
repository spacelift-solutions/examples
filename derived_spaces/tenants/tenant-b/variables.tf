variable "team_labels" {
  type        = list(string)
  description = "Ownership labels for the data platform stack, injected via terraform.tfvars."
}

# Auto-populated by Spacelift via TF_VAR_spacelift_run_type.
variable "spacelift_run_type" {
  type    = string
  default = "TRACKED"
}
