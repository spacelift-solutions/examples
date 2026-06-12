# Trivial workload shared by every stack the tenants create. This example is
# about space placement and ordering, not workload content.
terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

variable "stack_note" {
  type    = string
  default = "derived-spaces workload"
}

resource "null_resource" "workload" {
  triggers = {
    note = var.stack_note
  }
}
