# Spacelift Examples

This repository contains practical examples demonstrating various Spacelift features and use cases. Each directory represents a specific scenario with complete configuration files and documentation.

## Examples

### [env vars](env_vars/)
This example demonstrates how to manage Spacelift environment variables using a YAML configuration file with the Spacelift Terraform provider.

### [extract labels](extract_labels/)
A Go tool that statically parses Terraform/OpenTofu HCL and reports the labels applied to stacks created via the `terraform-spacelift-stack` module.

### [derived spaces](derived_spaces/)
A centralized spaces-admin pattern: one admin stack derives Spacelift spaces from `team/*` labels (via the `extract-spaces` Go parser), owns all `spacelift_space` resources, and publishes a `spaces.json` (name → ID) back to the repo that tenant stacks read. Avoids the "create space if not exists" provider antipattern.

## Getting Started

1. Choose an example that matches your use case
2. Follow the README in the specific example directory
3. Adapt the configuration to your environment
4. Deploy using Spacelift

## Prerequisites

- A Spacelift account
- Terraform knowledge
- Access to your target cloud provider (AWS, Azure, GCP)

## Contributing

Feel free to contribute additional examples or improvements to existing ones.