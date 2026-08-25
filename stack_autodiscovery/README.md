# Stack Autodiscovery

This example demonstrates how to have one administrative stack create a Spacelift
stack for every directory it finds on disk, so that adding a stack becomes "add a
directory and open a pull request" instead of "write more Terraform".

## Overview

An administrative stack scans a directory in the repository with `fileset()` at
plan time. Every subdirectory that contains at least one file is turned into a
stack via the `stacks-module` module from the Spacelift module registry
(the [terraform-spacelift-stack](https://github.com/spacelift-solutions/terraform-spacelift-stack)
module, published to `spacelift.io`), with a name and an `env:` label derived
from the directory path.

Because the discovery happens in Terraform, no stack definitions are duplicated
and nothing has to be kept in sync by hand: the filesystem is the source of
truth.

## Prerequisites

The module is consumed from the Spacelift module registry:

```hcl
source = "spacelift.io/spacelift-solutions/stacks-module/spacelift"
```

That source resolves against your own account's registry, so `stacks-module`
needs to be published there before a run can initialise. Spacelift injects the
registry credentials automatically inside a run; a `terraform init` from a
laptop returns `401 Unauthorized` unless you configure credentials for
`spacelift.io` yourself.

## Structure

- `admin/autodiscovery.tf` - discovery logic and the `module` block that creates the stacks
- `admin/variables.tf` - inputs (source root, scanned directory, repository, space)
- `admin/outputs.tf` - the discovered stack map, handy for inspecting a plan
- `admin/providers.tf` - Spacelift provider configuration
- `autodiscovery/` - the scanned directory; each subdirectory here becomes a stack

The sample tree ships three directories:

```
autodiscovery/
├── dev/
│   ├── app/main.tf
│   └── network/main.tf
└── prod/
    └── network/main.tf
```

## How it Works

1. **Locate the source on disk.** Inside a Spacelift run the repository is always
   checked out at `/mnt/workspace/source/`, which is the default of
   `var.source_root`. This is the whole repository, not the stack's
   `project_root`, so `var.autodiscover_directory` is relative to the repository
   root.

2. **Find the directories.** `fileset()` returns every file under the scanned
   directory, `dirname()` reduces each to its directory, and `distinct()` removes
   the duplicates that come from directories holding more than one file:

   ```hcl
   discovered_directories = [
     for dir in distinct([
       for _, v in fileset("${local.abs_source}${local.autodiscover_directory}", "**") : dirname(v)
     ]) : dir if dir != "."
   ]
   ```

   `fileset()` only ever returns files, so an empty directory is never
   discovered. `"."` is filtered out because it represents a file sitting
   directly in the scanned directory (a `README.md`, for example) rather than a
   stack.

3. **Derive each stack's attributes.** The directory path becomes a map key so
   `for_each` gets a stable identity per stack:

   ```hcl
   discovered_stacks = {
     for k, v in local.discovered_directories : v =>
     {
       path : "${local.autodiscover_directory}/${v}",
       env : regex("([a-z]+)", v)[0],
       stack_name : "auto-discovered-${replace(v, "/", "-")}"
     }
   }
   ```

   - `path` is the stack's `project_root`, relative to the repository root
   - `env` is the first run of lowercase letters in the path, i.e. the top-level
     directory name
   - `stack_name` flattens the path, so `dev/network` becomes
     `auto-discovered-dev-network`

4. **Create the stacks.** The module is called once per entry, and each stack is
   labelled with its environment plus `autodiscovered`, which makes the generated
   stacks easy to find and easy to target with policies.

For the sample tree the resulting map is:

```hcl
discovered_stacks = {
  "dev/app" = {
    "env"        = "dev"
    "path"       = "stack_autodiscovery/autodiscovery/dev/app"
    "stack_name" = "auto-discovered-dev-app"
  }
  "dev/network" = {
    "env"        = "dev"
    "path"       = "stack_autodiscovery/autodiscovery/dev/network"
    "stack_name" = "auto-discovered-dev-network"
  }
  "prod/network" = {
    "env"        = "prod"
    "path"       = "stack_autodiscovery/autodiscovery/prod/network"
    "stack_name" = "auto-discovered-prod-network"
  }
}
```

## Usage

1. Create the administrative stack in Spacelift, pointing at this repository with
   `stack_autodiscovery/admin` as the project root, and mark it as administrative
   so it is allowed to manage Spacelift resources.

2. Point `var.autodiscover_directory` at the directory you want scanned and
   `var.repository_name` at your repository. The defaults match this example
   (`stack_autodiscovery/autodiscovery` in the `examples` repository).

3. Trigger a run. The administrative stack creates one stack per discovered
   directory.

4. To add a stack, add a directory with at least one file in it and merge. To
   remove a stack, delete its directory: the next administrative run destroys the
   stack.

Because discovery reads the filesystem, the administrative stack must re-run
whenever the scanned tree changes. Give it a push policy that triggers on changes
under both `stack_autodiscovery/admin` and `stack_autodiscovery/autodiscovery`,
or leave it on the default behaviour of tracking the whole project.

### Inspecting discovery outside of Spacelift

`var.source_root` exists because `/mnt/workspace/source/` only exists inside a
Spacelift run. Point it at a checkout to see what would be discovered, for
example from `admin/`:

```bash
terraform console -var 'source_root=../../'
```

then evaluate `local.discovered_stacks`. This needs `terraform init` to have
succeeded, so it only works where the module registry is reachable (see
Prerequisites). Otherwise, read the `discovered_stacks` output from the
administrative stack's run in Spacelift, which reports the same map.

## Things to Watch Out For

- **Directory names should be lowercase.** `env` comes from
  `regex("([a-z]+)", v)`, which matches the first run of lowercase letters. A
  directory named `PROD/api` yields `env:api`, not `env:prod`, because `PROD` has
  no lowercase characters. A path with no lowercase letters at all makes the
  regex fail and the run error out.
- **Hidden files count as files.** `fileset()` with `**` matches dotfiles, so a
  directory containing only a `.keep` becomes a stack whose Terraform code is
  empty.
- **Renaming a directory replaces a stack.** The directory path is the `for_each`
  key, so a rename is a destroy plus a create, not an update. Move the state or
  accept the recreation.
- **Discovery happens at plan time.** The stack list reflects the commit the
  administrative run is planning, which is why the administrative stack needs to
  run on changes to the scanned directory.
