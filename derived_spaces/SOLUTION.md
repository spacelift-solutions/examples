# Centralized Spaces-Admin: Design Rationale & Validation

Design notes and validation results for the centralized spaces-admin pattern in
this example. The companion [README.md](README.md) is the runbook; this document
explains *why* the design is shaped the way it is and records the scenarios that
were validated end-to-end.

## TL;DR

When spaces are created on demand from team-ownership labels spread across many
admin stacks, it is tempting to make `spacelift_space` adopt pre-existing spaces.
That is the wrong fix. The alternative validated here: a single spaces-admin
stack that derives every required space by statically parsing the tenant
declarations, owns all `spacelift_space` resources, and publishes a `spaces.json`
(name → ID) back to the repo as the source of truth. Tenant stacks resolve their
space from that committed file — no data sources, no adoption, no races.

Two questions this design answers, both with evidence below:

1. **The unresolved-labels problem is solved.** The parser evaluates label
   expressions (locals, vars, tfvars, `concat`, `jsondecode(file(...))` ownership
   lookups) instead of only accepting literals. Against the same fixtures, a
   literal-only approach resolves 1 of 4 module calls; this parser resolves 4 of
   4 and fails loudly (file:line) on anything it can't resolve.
2. **The PR experience is acceptable and tunable.** By default, a PR introducing
   a new team combination shows a green spaces-admin check that *previews the
   exact space to be created* and a red tenant check with a self-explanatory
   message; everything self-heals after merge with zero human action. With an
   optional run-type-aware precondition (validated as scenario 8), those PRs are
   fully green.

## The problem

- A space is derived per stack from its owning + collaborating teams: one team →
  `teama`, combos → `custom-teama-teamb` (lowercased, sorted).
- Any stack in a large fleet may be the first to need a given space. A shared
  module doing a `data "spacelift_spaces"` label lookup with a create-fallback
  fails, because the provider (correctly) refuses to create a space that already
  exists.
- The tempting ask is to make the provider "not error if the space exists" —
  i.e., adopt. Adoption is the wrong answer: it masks drift, and is destructive
  on destroy (the first stack to be destroyed deletes a space every other stack
  depends on). The language-level answer to "already exists" is import blocks,
  not silent adoption.
- A second blocker: a literal-only HCL parser leaves a large fraction of module
  calls unresolved because ownership labels arrive through arbitrary variable
  wiring.

## Architecture

```
administrative (existing management stack, root space)
  └─ space "platform" + 3 stacks + role attachments + 2 stack dependencies
       ├─ spaces-admin                      (root space, Space admin on root)
       │    before_init:  install Go        (example-grade; bake a runner image in prod)
       │    before_plan:  run extract-spaces over ../tenants -> desired_spaces.json
       │    terraform:    spacelift_space for_each
       │                  github_repository_file -> commits spaces.json to main
       ├─ tenant-a / tenant-b               (platform space, scoped Space admin)
       │    component-stack module: team/<name> labels -> space name
       │                  -> lookup committed spaces.json -> spacelift_stack
       └─ workload stacks                   (created INTO the team spaces)
```

**Two JSON files, deliberately distinct:**

| File | Content | Lifecycle |
|---|---|---|
| `desired_spaces.json` | space → teams/description | parser output, regenerated every run, never committed |
| `spaces.json` | space → space ID | committed to main **by the spaces-admin apply**; the source of truth tenants read |

**Trigger topology** — the anti-loop invariant is that `spaces.json` matches the
tenants' `additional_project_globs` but not spaces-admin's, so the publish commit
retriggers tenants only:

| Path changed | Runs triggered |
|---|---|
| `tenants/**`, `extract-spaces/**` | spaces-admin (+ the touched tenant) |
| `spaces.json` (published by spaces-admin) | tenants only — never spaces-admin |
| `modules/component-stack/**` | both tenants |

## The parser (the unresolved-labels answer)

`extract-spaces` (Go, hcl/v2 + go-cty) builds a **static evaluation context per
tenant root** — variable defaults overlaid with tfvars, locals resolved to a
fixpoint, the cty stdlib, and `file()`/`jsondecode()` rooted at the tenant dir —
then evaluates the `labels` argument of every component-stack module call. No
terraform execution needed. Anything still unresolved exits 1 with file:line and
the offending expression: unresolved ownership is a build failure, never a silent
skip.

**Evidence (same four module calls, both tools):**

Old literal-only parser (`extract_labels`):
```
module "svc_alpha_dev"          labels: <unresolved expression> local.common_team_labels
module "svc_payments_dev"       labels: team/teamA, team/teamB
module "svc_data_platform_dev"  labels: <unresolved expression> var.team_labels
module "svc_search_dev"         labels: <unresolved expression> concat(local.base_labels, ["team/teamA"])
Found 4 module(s); 3 with unresolved label expressions.
```

extract-spaces:
```
tenant tenant-a: module "svc_alpha_dev" (main.tf)          -> space "teama"                    (via jsondecode(file(owners.json)) lookup)
tenant tenant-a: module "svc_payments_dev" (main.tf)       -> space "custom-teama-teamb"       (literal)
tenant tenant-b: module "svc_data_platform_dev" (main.tf)  -> space "custom-teama-teamb-teamc" (var + terraform.tfvars)
tenant tenant-b: module "svc_search_dev" (main.tf)         -> space "teama"                    (concat(local, inline))
3 space(s) required across 4 module call(s).
```

The space-name derivation intentionally lives twice — Go (parser) and HCL
(module) — and is pinned by a golden test in `extract-spaces/main_test.go`.
Divergence is the #1 maintenance hazard; in production, generate one from the
other or add a `tofu test` parity suite.

## Validation results

All scenarios were run live against a Spacelift account.

### 1. Baseline provisioning ✅

First spaces-admin run: installed Go on the worker, ran the parser (4 calls → 3
spaces), created the spaces, committed `spaces.json`. That commit auto-triggered
both tenants, which created 4 workload stacks in the correct spaces — including
the dedupe proof: stacks from *both* tenants resolved the identical `teama` space
ID.

### 2. PR introducing a new team combination (`teamA+teamD`) ✅

The key question. Observed:

| Phase | Check / run | Result |
|---|---|---|
| PR open | `spacelift/spaces-admin` | **pass** — plan previews `+ spacelift_space.team["custom-teama-teamd"]` and the `spaces.json` diff |
| PR open | `spacelift/tenant-b` | **fail** — precondition (below), expected |
| Merge | spaces-admin tracked | **FINISHED** — creates space, publishes `spaces.json` |
| Merge | tenant-b tracked (dependency-queued) | **FAILED** — pinned to merge commit, stale `spaces.json` |
| Publish | tenant-b tracked | **FINISHED** — stack created in the new space |

The precondition message developers see on the PR:

```
Space "custom-teama-teamd" is not in derived_spaces/spaces.json yet.
The spaces-admin stack creates it and commits the updated
spaces.json to main; this stack is then retriggered automatically by
that commit. If this is a proposed (PR) run introducing a new team
combination, this failure is expected and self-heals after merge.
```

Net steady-state cost per new combo: one red proposed run + one red tracked run,
both self-explanatory, healing with **zero human action**. (Stack dependencies do
not apply to proposed runs, confirmed empirically.)

### 3. Same new combo in both tenants, one PR (`teamA+teamE`) ✅

spaces-admin previewed **exactly one** new space:
`description = "Shared space for teama+teame. Used by: tenant-a, tenant-b."`
After the heal, both tenants' stacks resolved the identical space ID. No
duplicate-space race is possible — there is exactly one writer.

### 4. Benign change to an existing-combo stack ✅ (+ a real-world finding)

After rebase: tenant-b check **pass**, spaces-admin check **skipping** (no space
changes → Spacelift skips the no-op proposed run). Tracked run queued briefly
behind spaces-admin's no-op (dependency ordering), then applied green. **PRs only
go red when a new combination is introduced.**

> **Finding — stale-base PRs:** the first attempt at this PR failed because the
> branch was cut in the window between scenario 3's merge and its `spaces.json`
> publish commit — so the branch's `spaces.json` predated `custom-teama-teame`
> and the precondition fired for an *unrelated, already-merged* stack. A rebase
> on main fixed it. At scale this window is hit routinely: **document "rebase on
> main" as the first response to an unexpected missing-space failure**, or adopt
> the scenario-8 variant which eliminates the failure mode entirely.

### 5. Combo removal (removes `teamA+teamD`) ✅ (two-phase, by design)

- Proposed runs: both green; spaces-admin previews the space destroy.
- On merge: spaces-admin **FAILED** — `could not delete space: cannot delete
  space. this entity has remaining references to it: Stack (svc-reporting-dev)` —
  and tenant-b was **SKIPPED** (dependencies skip children when the parent fails,
  confirmed empirically).
- Recovery: manually trigger tenant-b (destroys the workload stack), then
  spaces-admin (destroys the now-empty space, publishes updated `spaces.json`).

**Operational caveat:** removal is inherently two-phase (stack first, space
second) and the dependency's skip-on-parent-failure means it does not self-heal —
it needs a retrigger. Options for production: accept the manual step (removals are
rare), or have spaces-admin retain spaces and only garbage-collect empties on a
schedule.

### 6. Manual edit of `spaces.json` on main ✅

Committed a bogus key directly to main. Tenants retriggered (green no-ops — real
keys untouched). Next spaces-admin run detected the drift on
`github_repository_file` and **reverted the file to canonical content**
(`Plan: 0 to add, 1 to change, 0 to destroy`). The source of truth defends
itself; note that a malicious/bad edit *would* be live for tenant runs until the
next spaces-admin run.

### 7. Brand-new tenant directory in a PR ✅

`tenants/tenant-c` exists on a branch only; no Spacelift stack tracks it.
spaces-admin's `tenants/**` glob still produced a proposed run previewing the new
space: `+ spacelift_space.team["custom-teamb-teamc"] ... Used by: tenant-c.`
Onboarding a tenant still requires one `administrative/` change to create its
tenant stack — the glob only buys the *space* preview.

### 8. Optional: run-type-aware precondition ✅

Relaxing the precondition on `PROPOSED` runs (via the natively injected
`TF_VAR_spacelift_run_type`) makes new-combo PRs **fully green**: tenant-a's check
passed, planning the stack with `space_id = (known after apply)`. Tracked runs
keep the hard failure + self-heal. **Trade-off:** a green PR no longer proves the
stack is immediately deployable. Recommendation: start with the default
(red-but-explained); flip to this variant if devs find the red checks noisy. It
also eliminates the scenario-4 stale-base failure mode.

### 9. Parser comparison ✅

See "The parser" above: old approach 1/4 resolved, this parser 4/4, with loud
failure as the contract (a deliberate negative fixture proves unresolvable wiring
exits non-zero naming the file and expression).

## Productionizing this

1. **Runner image with Go baked in** (or a prebuilt parser binary). This example
   installs the Go toolchain in a `before_init` hook — fine for a demo, not for a
   large fleet.
2. **Adapt the parser's label convention** — `-team-prefix` and the module's
   filter are the only two places encoding `team/<name>`; map to your actual
   schema.
3. **Fine-grained PAT** for the `spaces.json` publish (Contents r/w on the one
   repo). Anyone who can edit the spaces-admin stack's hooks can read it — scope
   accordingly. Branch protection that blocks the PAT's direct pushes breaks
   publishing.
4. **Parity discipline**: the Go/HCL dual derivation is pinned by a golden test;
   wire it into CI.
5. **Removal runbook**: two-phase (stack, then space) with a manual retrigger, or
   scheduled GC of empty spaces.
6. **Platform constraints**: space-tree management requires root-scoped
   `SPACE_ADMIN` on a **root-space** stack; root-scoped role attachments are only
   allowed on root-space stacks.
