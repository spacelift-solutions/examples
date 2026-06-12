terraform {
  required_providers {
    spacelift = {
      source  = "spacelift-io/spacelift"
      version = ">=1.17.0"
    }
    github = {
      source  = "integrations/github"
      version = "~> 6.0"
    }
  }
}

# Token comes from the GITHUB_TOKEN environment variable on the stack
# (fine-grained PAT, this repo only, Contents read/write).
provider "github" {
  owner = "your-github-org"
}
