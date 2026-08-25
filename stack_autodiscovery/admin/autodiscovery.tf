locals {
  # Where the source code is located on disk. Inside Spacelift this is always
  # /mnt/workspace/source/, so var.source_root only changes for local plans.
  abs_source = var.source_root

  # Path where auto discovered stacks will be located.
  # Must be from the root of the repository.
  autodiscover_directory = var.autodiscover_directory

  # Get all unique directories with files in them from the autodiscovery
  # directory. fileset() only returns files, so a directory without any files
  # in it is never discovered. "." is the autodiscovery directory itself and is
  # dropped: it is not a stack.
  discovered_directories = [
    for dir in distinct([
      for _, v in fileset("${local.abs_source}${local.autodiscover_directory}", "**") : dirname(v)
    ]) : dir if dir != "."
  ]

  # Break the discovered directories out into a map of
  # directory => {path, env, stack_name}
  discovered_stacks = {
    for k, v in local.discovered_directories : v =>
    {
      path : "${local.autodiscover_directory}/${v}",
      env : regex("([a-z]+)", v)[0],
      stack_name : "auto-discovered-${replace(v, "/", "-")}"
    }
  }
}

module "autodiscovered_stack" {
  source = "spacelift-solutions/stack/spacelift"

  for_each = local.discovered_stacks

  name              = each.value.stack_name
  description       = "Autodiscovered stack"
  repository_name   = var.repository_name
  repository_branch = var.repository_branch
  project_root      = each.value.path

  labels = ["env:${each.value.env}", "autodiscovered"]

  space_id = var.space_id
}
