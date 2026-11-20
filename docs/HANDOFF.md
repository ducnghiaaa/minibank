# minibank — Handoff

Where the project is and what to do next. Update this file at the end of each work session.
Public repo: no secrets here.

_Last updated: 2026-11-19_

## Status

| Part | State |
|---|---|
| App design spec | Approved (2026-10-26) |
| Infra design spec | Draft, domain set |
| Part 0 — accounts, state, org, CI login | **Done** |
| Persistent stack — `dns` | **Done** (zone imported, PR #2) |
| Persistent stack — `network` | **Next** — console lab, step 1 (VPC) not started |
| App code | Not started |

## Repos

| Repo | Visibility | What |
|---|---|---|
| [`minibank`](https://github.com/ducnghiaaa/minibank) | public | App code, specs, ADRs, this file |
| [`minibank-infra`](https://github.com/ducnghiaaa/minibank-infra) | public | Terraform |
| [`minibank-gitops`](https://github.com/ducnghiaaa/minibank-gitops) | private | Kubernetes manifests (empty) |

Specs: `docs/superpowers/specs/2026-10-26-minibank-{app,infra}-design.md`

## Key facts

| Item | Value |
|---|---|
| Accounts | management `722433231642`, workload `701254204984` |
| CLI profiles | `minibank-mgmt`, `minibank-workload` (sso-session `minibank`) |
| Region | `ap-southeast-1` |
| Domain | `andrewnguyen.cloud` (GoDaddy → Route 53 in workload) |
| Hosted zone | `Z0524474HG5AIAQLAIUA` (`prevent_destroy`) |
| State bucket (workload) | `minibank-tfstate-701254204984` |
| CI roles | `tf-plan` (PRs, main), `tf-apply` (environment `dev`, not used yet) |
| Budget | $40/month (management account) |

## What is in `minibank-infra`

```
bootstrap/{management,workload}    S3 state buckets
management/organization            budget, Workloads OU, guardrail SCP
workload/dev/persistent/ci-oidc    GitHub OIDC provider, tf-plan, tf-apply
workload/dev/persistent/dns        hosted zone (imported)
modules/state-bucket
.github/workflows/terraform-pr.yml plan on PR, one job per stack (matrix)
```

## How we work

1. **Console first (lab):** build the resource by hand, step by step: why → do → check.
2. **Cleanup:** delete what you built, in the right order (learn the dependencies).
3. **Then code:** Claude writes Terraform that builds it from scratch; plan shows only `add`.
   `import` only as an exception, when recreating would break things (the `dns` zone).
4. Claude writes code; the user reviews, commits, pushes, and merges. One small PR per stack.

## Lessons learned

- **GitHub OIDC immutable subject:** this repo's `sub` claim is
  `repo:ducnghiaaa@129480468/minibank-infra@1459049748:...`, not `repo:ducnghiaaa/minibank-infra:...`.
  Trust policies must use the ID form. Check with
  `gh api repos/<owner>/<repo>/actions/oidc/customization/sub`.
- **Console defaults ≠ Terraform defaults:** Route 53 comment (console empty, Terraform
  "Managed by Terraform"), and provider `default_tags` add tags on import. Set them in code.
- **A CI plan that runs before your local apply shows old results.** Re-run the job.

## Next steps

1. **Network lab** (spec §5), all free — no NAT here (NAT is ephemeral):

   | Item | ap-southeast-1a | ap-southeast-1b |
   |---|---|---|
   | VPC `minibank-dev` | `10.0.0.0/16` | |
   | Private `/19` | `10.0.0.0/19` | `10.0.32.0/19` |
   | Public `/24` | `10.0.64.0/24` | `10.0.65.0/24` |
   | DB `/26` | `10.0.66.0/26` | `10.0.66.64/26` |

   Plus internet gateway, route tables, S3 gateway endpoint. Step 1: VPC only, DNS hostnames on.
2. Then Terraform `workload/dev/persistent/network` + import.

## Open cleanups

- Spec §9.1 folder tree: add `dns/`.
- `default_tags`: add `env=dev` and `component=<folder>` (spec §9.1) from the next stack on.
- `dns/main.tf`: the `import` block is done; keep as record or remove.
- CI still missing from spec §10: `validate`, `tflint`, `trivy`, Infracost, plan only changed
  folders, apply on merge, weekly drift.
