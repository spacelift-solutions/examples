# A shared component-stack module, label-driven.
#
# The space is derived from the team/<name> labels and resolved against the
# committed derived_spaces/spaces.json — never via a data source, never by
# creating the space here. The spaces-admin stack is the sole owner of spaces
# and of spaces.json.
#
# PARITY: the teams/space_name derivation below is duplicated in Go in
# derived_spaces/extract-spaces (teamSet/SpaceName, pinned by its golden
# test). If you change one, change both.
locals {
  teams      = sort(distinct([for l in var.labels : lower(trimprefix(l, "team/")) if startswith(l, "team/")]))
  space_name = length(local.teams) == 1 ? local.teams[0] : "custom-${join("-", local.teams)}"
  spaces     = jsondecode(file("${path.root}/../../spaces.json"))
  space_id   = lookup(local.spaces, local.space_name, null)
}

resource "spacelift_stack" "this" {
  name         = var.name
  repository   = "your-repo"
  branch       = "main"
  project_root = "derived_spaces/workload"
  space_id     = local.space_id
  labels       = var.labels
  autodeploy   = true

  # Uses the account's default Spacelift GitHub App integration, so no VCS block
  # is needed. For a custom GitHub Enterprise/App integration instead, add the
  # matching block (e.g. github_enterprise { namespace = ..., id = ... }).

  worker_pool_id          = var.worker_pool_id
  terraform_workflow_tool = "OPEN_TOFU"
  terraform_version       = "1.9.0"

  lifecycle {
    precondition {
      condition     = length(local.teams) > 0
      error_message = "component-stack call \"${var.name}\" carries no team/<name> labels; ownership labels are required to derive its space."
    }
    # Relaxed on PROPOSED runs: a PR introducing a new combination plans green
    # (the previewed stack simply has no space yet). Tracked runs still hard-
    # fail until spaces-admin publishes the space, preserving the self-heal
    # flow. Trade-off: a green PR no longer proves immediate deployability.
    precondition {
      condition     = local.space_id != null || var.run_type == "PROPOSED"
      error_message = <<-EOT
        Space "${local.space_name}" is not in derived_spaces/spaces.json yet.
        The spaces-admin stack creates it and commits the updated
        spaces.json to main; this stack is then retriggered automatically by
        that commit. This tracked-run failure self-heals once that commit lands.
      EOT
    }
  }
}
