# Placeholder workload for the auto discovered "prod/network" stack. Replace this with
# whatever the stack should actually manage.
resource "null_resource" "this" {
  triggers = {
    directory = "prod/network"
  }

  provisioner "local-exec" {
    command = "echo running the prod/network stack"
  }
}
