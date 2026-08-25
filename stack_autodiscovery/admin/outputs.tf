output "discovered_stacks" {
  description = "The stacks that were discovered on disk, keyed by directory."
  value       = local.discovered_stacks
}
