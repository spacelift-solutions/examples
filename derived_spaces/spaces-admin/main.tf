# Centralized spaces admin: the SOLE owner of team spaces.
#
# desired_spaces.json is produced at run time by the extract-spaces parser
# (before_plan hook on this stack) from the tenant declarations — it is never
# committed. The committed artifact is derived_spaces/spaces.json (space name
# -> space ID), published below via the github provider; tenant stacks resolve
# their space from that file and are retriggered by its commit.
locals {
  desired = jsondecode(file("${path.root}/desired_spaces.json"))
}

resource "spacelift_space" "team" {
  for_each = local.desired.spaces

  name             = each.key
  parent_space_id  = var.parent_space_id
  description      = each.value.description
  inherit_entities = true
}

resource "github_repository_file" "spaces_json" {
  repository = "your-repo"
  # Explicit branch: the provider's default-branch detection is unreliable.
  branch              = "main"
  file                = "derived_spaces/spaces.json"
  content             = "${jsonencode({ for name, space in spacelift_space.team : name => space.id })}\n"
  commit_message      = "chore(spaces-admin): publish spaces.json from spaces-admin run"
  overwrite_on_create = true # bootstrap {} already exists in the repo
}

output "spaces" {
  value       = { for name, space in spacelift_space.team : name => space.id }
  description = "Space name -> ID map, as published to spaces.json."
}
