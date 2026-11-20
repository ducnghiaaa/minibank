# minibank — Handoff

Where the project is and what to do next. Update this file at the end of each work session.
Public repo: no secrets here.

_Last updated: 2026-11-20_

## Status

| Part | State |
|---|---|
| App design spec | Approved (2026-10-26) |
| Infra design spec | Draft, domain set |
| Part 0 — accounts, state, org, CI login | **Done** |
| Persistent — `ci-oidc` | **Done** (`tf-plan` tested; `tf-apply` not used yet) |
| Persistent — `dns` | **Done** (zone imported, PR #2) |
| Persistent — `network` | **Done** (VPC module, built from scratch, PR #3) |
| Persistent — rest | Not started — see Roadmap |
| Ephemeral (NAT, EKS, add-ons, ArgoCD) | Not started |
| App code | **Not started** — see Risk |

Nothing billed per hour is running. Always-on cost: hosted zone ~$0.50/month + S3 state (cents).

## Repos

| Repo | Visibility | What |
|---|---|---|
| [`minibank`](https://github.com/ducnghiaaa/minibank) | public | App code, specs, ADRs, this file, `CLAUDE.md`, skills |
| [`minibank-infra`](https://github.com/ducnghiaaa/minibank-infra) | public | Terraform, own `CLAUDE.md` |
| [`minibank-gitops`](https://github.com/ducnghiaaa/minibank-gitops) | private | Kubernetes manifests (empty) |

Specs: `docs/superpowers/specs/2026-10-26-minibank-{app,infra}-design.md` · ADRs: `docs/adr/`

## Key facts

| Item | Value |
|---|---|
| Accounts | management `722433231642`, workload `701254204984` |
| CLI profiles | `minibank-mgmt`, `minibank-workload` (sso-session `minibank`) |
| Region | `ap-southeast-1` |
| Domain | `andrewnguyen.cloud` (GoDaddy → Route 53 in workload) |
| Hosted zone | `Z0524474HG5AIAQLAIUA` (`prevent_destroy`) |
| VPC | `minibank-dev` `vpc-0fb4a5696500bc462`, `10.0.0.0/16`, 2 AZ |
| State bucket (workload) | `minibank-tfstate-701254204984` |
| CI roles | `tf-plan` (PRs, main), `tf-apply` (environment `dev`, not used yet) |
| Budget | $40/month (management account) |

## What is in `minibank-infra`

```
bootstrap/{management,workload}    S3 state buckets
management/organization            budget, Workloads OU, guardrail SCP
workload/dev/persistent/ci-oidc    GitHub OIDC provider, tf-plan, tf-apply
workload/dev/persistent/dns        hosted zone (imported)
workload/dev/persistent/network    VPC module 6.7.3: public/private/db subnets, IGW,
                                   1 private + 1 db route table, DB subnet group, S3 endpoint
modules/state-bucket
.github/workflows/terraform-pr.yml fmt + plan on PR, one job per stack (matrix)
```

## How we work

1. **Console first (lab):** build the resource by hand, step by step: why → do → check.
2. **Cleanup:** delete what you built, in the right order (learn the dependencies).
3. **Then code:** Claude writes Terraform that builds it from scratch; plan shows only `add`.
   `import` only as an exception, when recreating would break things (the `dns` zone).
4. Claude writes code; the user reviews, commits, pushes, and merges. One small PR per stack.
5. Skills in `.claude/skills/`: `aws-lab`, `codify`, `handoff`, `adr`.

## Lessons learned

- **GitHub OIDC immutable subject:** this repo's `sub` claim is
  `repo:ducnghiaaa@129480468/minibank-infra@1459049748:...`, not `repo:ducnghiaaa/minibank-infra:...`.
  Check with `gh api repos/<owner>/<repo>/actions/oidc/customization/sub`.
- **Console defaults ≠ Terraform defaults:** Route 53 comment, and `default_tags` add tags on import.
- **A CI plan that runs before your local apply shows old results.** Re-run the job.
- **`DependencyViolation`:** delete in reverse build order — route table needs its subnet
  associations removed first; an IGW must be detached before delete. Expect the same in `down`.
- **VPC module trick:** `enable_nat_gateway = false` + `single_nat_gateway = true` → one shared
  private route table, which the ephemeral `nat` stack will add its route to.

## Next step

Choose the next track (see Risk): **(a)** next persistent infra piece, or **(b)** start app code
in parallel. Suggested first infra piece: `registry` (ECR) — small, nearly free, needed by app CI.

## Roadmap — what is left for phase 1 (~end of 12/2026)

Spec §-numbers refer to the infra spec unless marked "app".

**Persistent infra** (each: lab → cleanup → Terraform)
- [ ] `registry` — ECR repo per service, immutable tags, scan on push, keep 20 images (§8)
- [ ] `messaging` — SNS `transactions` → SQS `notification` + DLQ (§8)
- [ ] `identity` — Cognito user pool (Essentials, Managed Login) + clients `web`, `e2e-test` (§8)
- [ ] `data` — KMS keys (ES256 sign, HMAC pepper), Aurora Serverless v2 (0–2 ACU), Secrets Manager (§8)
- [ ] `edge` — ACM certs (us-east-1 + ap-southeast-1), S3 SPA bucket, CloudFront, `X-Origin-Verify` secret (§7)
- [ ] IAM roles for workloads — services, ESO, LB Controller, Karpenter, external-dns, Grafana Cloud (§4)
- [ ] CI roles `lifecycle` and `app-ci` (§10)

**Ephemeral infra** (`up` / `down` only)
- [ ] `nat` — fck-nat `t4g.nano` + route in the private route table (§5)
- [ ] `eks` — cluster, access entries, managed add-ons, 1 × `t4g.medium` system node (§6)
- [ ] `addons-iam` — Pod Identity associations, Karpenter interruption queue (§4, §6)
- [ ] `argocd-bootstrap` — ArgoCD + root app + GitOps Bridge (§9.2)

**GitOps** (`minibank-gitops`)
- [ ] Add-ons: Karpenter, LB Controller, Envoy Gateway, External Secrets, external-dns, Alloy, ArgoCD (§9.2)
- [ ] Kustomize base + overlay for each service; migration Jobs as `PreSync` hooks
- [ ] DB bootstrap Job (databases + IAM users per service) (§8)

**App** (`minibank`, app spec)
- [ ] Local dev: `compose.yaml`, Postgres, SQS stand-in (app §11)
- [ ] `security` (Go) — PIN Argon2id + pepper, ES256 transaction tokens, JWKS (app §6)
- [ ] `account` (Go) — double-entry ledger, transfers, merchant pay, outbox (app §4–5)
- [ ] `notification` (Go) — SQS consumer, WebSocket (app §7)
- [ ] `frontend` — React + Vite + TS, Cognito login (app §11)
- [ ] App CI — lint, test, multi-arch build, Trivy, push ECR, bump gitops tag (§9.3)

**Infra CI and lifecycle**
- [ ] PR checks: `validate`, `tflint`, `trivy config`, Infracost, plan only changed folders (§10)
- [ ] Apply on merge with manual approval (`tf-apply`); weekly drift plan (§10)
- [ ] `up` / `down` workflows + orphan check; auto `down` at 01:00 + CodeBuild safety net at 02:00 (§11)
- [ ] Cost Anomaly Detection (§11)

**Observability**
- [ ] Grafana Cloud: Alloy `k8s-monitoring`, CloudWatch integration role, 4 phase-1 alerts (§12)

**Phase 1 acceptance** (§14): `up` from zero without manual steps (timed) · clean `down`, Aurora
data kept · no long-lived keys · PR has plan + scan + Infracost · app commit reaches the cluster
with no manual step · 01:00 auto-off tested once.

## Risk

About 6 weeks to the phase-1 defense. The infra list above is long and **no app code exists
yet**. Claude writes the code, so the app can move in parallel with the infra labs — but the
user still needs time to review and understand it. Decide the split soon.

## Open cleanups

- Spec §9.1 folder tree: add `dns/`.
- `ci-oidc` and `dns` providers: add `env=dev` and `component=<folder>` tags (network has them).
- `dns/main.tf`: the `import` block is done; keep as record or remove.
- Default VPC (`172.31.0.0/16`) still exists in Singapore — decide keep or delete.
- Local branch `dns` in `minibank-infra` is merged — `git branch -d dns`.
