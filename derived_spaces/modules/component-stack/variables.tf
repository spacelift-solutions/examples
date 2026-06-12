variable "name" {
  type        = string
  description = "Name of the workload stack."
}

variable "labels" {
  type        = list(string)
  description = "Stack labels. Ownership is encoded as team/<name> entries (at least one required); all labels are passed through to the stack."
}

variable "worker_pool_id" {
  type        = string
  default     = null
  nullable    = true
  description = "Worker pool the workload stack runs on. null = use the public worker pool."
}

variable "run_type" {
  type        = string
  default     = "TRACKED"
  description = "Spacelift run type (from TF_VAR_spacelift_run_type). On PROPOSED runs the missing-space precondition is relaxed so PRs introducing new team combinations stay green; the trade-off is that a green PR no longer proves the stack is immediately deployable."
}
