# Centralized Spaces, Derived From Team Labels

An architecture for migrating off label-based access policies onto Spaces
without a "create space if not exists" provider antipattern: a single
**spaces-admin** stack derives every required space from the tenant stack
declarations, owns all `spacelift_space` resources, and publishes a
`spaces.json` (space name → ID) back to the repo. Tenant stacks resolve their
space from that committed file — no data sources, no adoption.

## The problem this answers

A common pattern is to derive a space per stack from its owning + collaborating
teams (`teama` for one team, `custom-teama-teamb` for combos). Any of a large
fleet of admin stacks may be first to need a given space, which tempts you to
ask the provider to adopt pre-existing spaces — but adoption masks drift and is
destructive on destroy; the language-level answer to "already exists" is import
blocks, not silent adoption.

A second blocker: a literal-only HCL parser leaves a large fraction of real
module calls unresolved, because ownership labels arrive through `local.*`,
`var.*`, `concat(...)`, and JSON-file lookups. The `extract-spaces` parser here
resolves those by building a static evaluation context per tenant root
(variable defaults + tfvars, locals to fixpoint, cty stdlib, `file()` /
`jsondecode()`), and **fails loudly with file:line for anything still
unresolved** — never a silent skip.

## Architecture

```
administrative/                      (existing management stack, space root)
  └─ stacks_derived_spaces.tf: space "platform" + 3 stacks (each granted the
       │                   system Space admin role on platform) + 2 deps
       ├─ spaces-admin              (in platform)
       │    before_init:  install Go (example-grade; bake a runner image in prod)
       │    before_plan:  build + run extract-spaces over ../tenants
       │                  -> desired_spaces.json   (generated, gitignored)
       │    terraform:    spacelift_space for_each
       │                  github_repository_file -> commits spaces.json to main
       ├─ tenant-a / tenant-b       (in platform)
       │    component-stack module: team/<name> labels -> space name
       │                  -> lookup in committed spaces.json -> spacelift_stack
       └─ workload stacks           (in the team spaces, trivial null_resource)
```

**Two JSON files, deliberately distinct:**

| File | Content | Lifecycle |
|---|---|---|
| `spaces-admin/desired_spaces.json` | space name → teams/description | parser output, regenerated every run, gitignored |
| `spaces.json` | space name → space ID | committed to main by spaces-admin's apply; the source of truth tenants read |

**Trigger topology** (the anti-loop invariant: `spaces.json` is inside the
tenants' globs but *outside* spaces-admin's root and globs):

| Path changed | Runs |
|---|---|
| `tenants/**`, `extract-spaces/**` | spaces-admin (+ the touched tenant) |
| `spaces.json` (published by spaces-admin) | tenants only — never spaces-admin |
| `modules/component-stack/**` | both tenants |
| `workload/**` | workload stacks |

## Expected red runs (this is by design — read before panicking)

When a change introduces a **new team combination**:

1. **Proposed run (PR):** the tenant's plan fails its precondition — the space
   isn't in `spaces.json` at the PR's commit. spaces-admin's proposed run is
   green and previews exactly which space will be created plus the
   `spaces.json` diff. Stack dependencies do not apply to proposed runs.
2. **Tracked run (merge):** the dependency queues the tenant behind
   spaces-admin, but the tenant run is pinned to the merge commit, whose
   `spaces.json` still predates the new space → same precondition failure.
3. **Self-heal:** spaces-admin's apply commits the updated `spaces.json`,
   which retriggers the tenants at the new commit → green.

Net cost: one red proposed + one red tracked run per new combo, each carrying
a precondition message that explains all of the above.

## The stack definitions (live in your management stack, not this repo)

This repo ships the *components* — parser, spaces-admin root, module,
tenants. The stack definitions themselves (hooks, globs, env vars, roles) are
account-specific and belong in whatever management ("administrative") stack
creates them. The spaces-admin stack looks like this:

```hcl
resource "spacelift_space" "platform" {
  name             = "platform"
  parent_space_id  = "root"
  inherit_entities = true
}

resource "spacelift_stack" "spaces_admin" {
  name         = "spaces-admin"
  repository   = "your-repo"
  branch       = "main"
  project_root = "derived_spaces/spaces-admin"

  # Optional: a runner image with Go preinstalled removes the need for any
  # toolchain install hooks. Bake your own for production; this public one
  # works today as a convenience but isn't guaranteed to stay maintained:
  runner_image = "public.ecr.aws/o6n6e5l1/spacelift-runner-go:latest"

  # The anti-loop invariant: tenants/ and extract-spaces/ retrigger this
  # stack; the published spaces.json deliberately does NOT.
  additional_project_globs = [
    "derived_spaces/tenants/**",
    "derived_spaces/extract-spaces/**",
  ]

  # Hooks run in the project root (spaces-admin/), which is why the relative
  # paths below and the module's ${path.root} reads line up.
  before_plan = [
    "(cd ../extract-spaces && go build -o /tmp/extract-spaces .)",
    "/tmp/extract-spaces -dir ../tenants -source-match modules/component-stack -team-prefix team/ -out desired_spaces.json",
  ]
}

resource "spacelift_environment_variable" "parent_space_id" {
  stack_id   = spacelift_stack.spaces_admin.id
  name       = "TF_VAR_parent_space_id"
  value      = spacelift_space.platform.id
  write_only = false
}
```

No Go runner image? Add a `before_init` that installs the toolchain
(`curl -fsSL https://go.dev/dl/go1.25.0.linux-amd64.tar.gz -o /tmp/go.tar.gz
&& tar -C /tmp -xzf /tmp/go.tar.gz`) and call `/tmp/go/bin/go` in
`before_plan` — fine for a demo, not for a fleet.

Beyond these resources: grant the stack the system **Space admin** role on
root (space-tree management requires it on a root-space stack), and set a
write-only `GITHUB_TOKEN` on it (fine-grained PAT, Contents read/write on
this repo only) for the `spaces.json` publish. The tenant stacks are plain
stacks in the parent space whose `additional_project_globs` include
`derived_spaces/spaces.json` and `derived_spaces/modules/component-stack/**`.

## Runbook

1. Merge to main; confirm the administrative stack's tracked run (creates the
   space, three stacks, env var, dependencies).
2. Create a fine-grained PAT — **this repo only, Contents read/write** — and
   set it as a write-only `GITHUB_TOKEN` environment variable on the
   `spaces-admin` stack. Anyone who can edit that stack's hooks can read the
   token; scope accordingly. Branch protection on main that blocks the PAT's
   direct pushes breaks publishing.
3. Manually trigger `spaces-admin` (newly created stacks don't run on
   creation). Its apply creates the spaces and commits `spaces.json`; that
   commit triggers both tenants, which create the workload stacks in the team
   spaces.

### Teardown

Strictly in this order: tenant stacks destroy their workload stacks →
spaces-admin destroy (deletes spaces + removes `spaces.json`) → remove
`administrative/stacks_derived_spaces.tf`. Spaces refuse deletion while
occupied.

## Local verification

```sh
cd extract-spaces
go vet ./... && go test ./... && go build -o /tmp/extract-spaces .
/tmp/extract-spaces -dir ../tenants -source-match modules/component-stack -team-prefix team/ -out -
```

Expected: 7 module calls → 5 spaces — `teama` and `custom-teama-teame` each
shared by both tenants (the dedupe proof), plus `custom-teama-teamb`,
`custom-teama-teamb-teamc`, and `custom-teama-teamf`. (The `extract-spaces`
golden test runs against the trimmed `testdata/` fixtures instead: 4 calls →
3 spaces.)

## Production notes

- **Bake a runner image** with Go (or ship a prebuilt parser binary); the
  curl-Go-tarball hook is example-grade only.
  `public.ecr.aws/o6n6e5l1/spacelift-runner-go:latest` is a ready-made
  convenience, but pin or bake your own rather than depending on it.
- **The derivation logic lives twice** — Go (`extract-spaces`) and HCL
  (`modules/component-stack`). The golden test in `extract-spaces` pins parity;
  treat any change to one without the other as a broken build. For production,
  generate one from the other or add a `tofu test` parity suite.
- Your real label schema may differ; the parser's `-team-prefix` and the
  module's filter are the only two places encoding the convention.
- Manual edits to `spaces.json` on main propagate to tenants until
  spaces-admin's next run reverts them (drift demo: validation #6).

## Validation matrix

All scenarios below were validated end-to-end against a Spacelift account.
Full evidence and design analysis are in [SOLUTION.md](SOLUTION.md).

| # | Scenario | Expected | Result |
|---|---|---|---|
| 1 | Baseline merge + first spaces-admin trigger | 3 spaces, 4 workload stacks, spaces.json populated | ✅ |
| 2 | PR adds combo teamA+teamD to tenant-b | proposed: spaces-admin green (+space, spaces.json diff), tenant-b red precondition; merge: red→self-heal→green | ✅ |
| 3 | Same new combo in both tenants, one PR | one space, both tenants resolve same ID | ✅ |
| 4 | Benign tenant change (existing combo) | all green; measure dependency queue delay | ✅ |
| 5 | Combo removed | space destroy blocked while occupied; two-step teardown works | ✅ |
| 6 | Manual edit of spaces.json on main | tenants pick it up; spaces-admin reverts drift | ✅ |
| 7 | New tenant dir in a PR | spaces-admin proposed run previews new spaces; tenant stack still needs administrative/ change | ✅ |
| 8 | (Optional) SPACELIFT_RUN_TYPE-aware precondition | green PRs; trade-off: PR no longer proves deployability | ✅ |
| 9 | Old extract_labels vs extract-spaces on the same tenants | v1: styles 2-4 unresolved; v2: all resolved | ✅ |
