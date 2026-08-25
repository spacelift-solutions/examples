# Placeholder workload for the auto discovered "dev/network" stack. Replace this with
# whatever the stack should actually manage.
resource "null_resource" "this" {
  triggers = {
    directory = "dev/network"
  }

  provisioner "local-exec" {
    command = "echo running the dev/network stack"
  }
}
