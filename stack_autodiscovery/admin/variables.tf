variable "source_root" {
  type        = string
  description = <<-EOT
    Absolute path to the root of the checked out repository. Inside a Spacelift
    run this is always /mnt/workspace/source/ and should be left alone. Override
    it when planning from a laptop, e.g. -var 'source_root=../../../'.
  EOT
  default     = "/mnt/workspace/source/"
}

variable "autodiscover_directory" {
  type        = string
  description = <<-EOT
    Directory that is scanned for stacks, relative to the root of the
    repository. Every subdirectory of it that contains at least one file
    becomes a stack.
  EOT
  default     = "stack_autodiscovery/autodiscovery"
}

variable "repository_name" {
  type        = string
  description = "Repository the discovered stacks are created against."
  default     = "examples"
}

variable "repository_branch" {
  type        = string
  description = "Branch the discovered stacks track."
  default     = "main"
}

variable "space_id" {
  type        = string
  description = <<-EOT
    Space the discovered stacks are created in. When the administrative stack
    itself runs in Spacelift you can instead point this at the
    spacelift_current_space data source, as the other examples in this repo do.
  EOT
  default     = "root"
}
