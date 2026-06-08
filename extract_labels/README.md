# Extract stack labels from HCL

A small Go tool that statically parses Terraform/OpenTofu HCL and reports the
`labels` applied to every stack created via the
[`spacelift-solutions/terraform-spacelift-stack`](https://github.com/spacelift-solutions/terraform-spacelift-stack)
module.

It walks a directory, finds `module` blocks whose `source` matches the target
module, and pulls out the `labels` attribute for each one.

## What it can and can't resolve

This is a **static** parser — it reads the HCL, it does not run Terraform. That
distinction matters for how labels are written:

| How `labels` is written | Result |
|---|---|
| `labels = ["env:prod", "team:platform"]` | ✅ resolved to actual values |
| `labels = local.common_labels` | ⚠️ reported as unresolved; raw expression shown |
| `labels = concat(local.base, ["x"])` | ⚠️ reported as unresolved; raw expression shown |
| `labels = var.labels` | ⚠️ reported as unresolved; raw expression shown |

Anything that's a reference or function call needs a full Terraform evaluation
context (variables, locals, module wiring) to resolve, which a static parse
can't provide. Those stacks are flagged `resolved: false` with the verbatim
expression text so you can see what's going on.

**If you need the actually-applied labels on live stacks** (including dynamic
ones), query the Spacelift API or read state instead — that's the real source
of truth. This tool is for auditing the HCL itself.

## Usage

```bash
# Human-readable summary (defaults to current directory)
go run . -dir /path/to/terraform

# JSON output for piping into jq or another tool
go run . -dir /path/to/terraform -json

# Match a different module source substring
go run . -dir /path/to/terraform -source-match "terraform-spacelift-stack"
```

### Try it against the bundled fixtures

```bash
go run . -dir ./example_stacks
```

```
module "prod_api" (example_stacks/stacks.tf)
  source: github.com/spacelift-solutions/terraform-spacelift-stack
  labels: env:prod, team:platform, service:api

module "staging_api" (example_stacks/stacks.tf)
  source: github.com/spacelift-solutions/terraform-spacelift-stack
  labels: <unresolved expression> local.common_labels

module "worker" (example_stacks/stacks.tf)
  source: github.com/spacelift-solutions/terraform-spacelift-stack?ref=v1.2.0
  labels: <unresolved expression> concat(local.common_labels, ["env:prod", "service:worker"])

Found 3 module(s); 2 with unresolved label expressions.
Unique literal labels across all stacks (3): env:prod, service:api, team:platform
```

## How it works

- [`hashicorp/hcl/v2`](https://github.com/hashicorp/hcl) parses each `.tf` file
  into a native-syntax AST.
- `module` blocks are filtered by their `source` attribute.
- The `labels` expression is evaluated with an empty context via
  `expr.Value(nil)`. Literals evaluate cleanly; references/functions return
  diagnostics, which is how we detect the unresolved case and fall back to the
  raw source text.

`ScanDir` is exported so you can import this package and reuse the extraction
logic programmatically rather than only via the CLI.
