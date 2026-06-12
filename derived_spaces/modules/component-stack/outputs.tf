output "stack_id" {
  value       = spacelift_stack.this.id
  description = "ID of the created workload stack."
}

output "space_name" {
  value       = local.space_name
  description = "Derived space name; must match the extract-spaces parser output for the same labels."
}

output "space_id" {
  value       = local.space_id
  description = "Space ID resolved from spaces.json."
}
