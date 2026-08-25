# Placeholder workload for the auto discovered "dev/app" stack. Replace this with
# whatever the stack should actually manage.
resource "null_resource" "this" {
  triggers = {
    directory = "dev/app"
  }

  provisioner "local-exec" {
    command = "echo running the dev/app stack"
  }
}
