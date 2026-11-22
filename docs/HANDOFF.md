# minibank — Handoff

Where the project is and what to do next. Update this file at the end of each work session.
Public repo: no secrets here.

_Last updated: 2026-11-22_

## Status

| Part | State |
|---|---|
| App / infra design specs | Approved / draft (domain set) |
| Part 0 — accounts, state, org, CI login | **Done** |
| Persistent — `ci-oidc`, `dns`, `network` | **Done** |
| Persistent — `registry` (ECR) | **Done** (PR infra #4) |
| Persistent — `messaging` (SNS → SQS + DLQ) | **Done** (PR infra #5) |
| Persistent — `identity` (Cognito) | **In progress** — lab step 1 not started |
| App — `account` PR 1–4 | **Done**: skeleton, ledger schema, account opening, transfers |
| App — `account` PR 5 | Next: recipient lookup + history (preview approved in prototype) |
| Ephemeral (NAT, EKS, add-ons, ArgoCD) | Not started |

**Running cost:** nothing billed per hour. Always-on: hosted zone ~$0.50/month, S3 state, ECR
storage (empty), SNS/SQS (free tier). AWS last checked 2026-11-20 — the SSO session had expired
on 2026-11-22. If a Cognito lab pool was created: still $0 (Essentials free tier, 10,000 MAU).

## Prototype (preview of everything)

`docs/superpowers/specs/2026-11-20-minibank-prototype.html` — one HTML file, open it in a browser.
Tabs: App (clickable mock), Hạ tầng (status colours), Việc sắp làm (next-task previews),
Lab SNS/SQS. Also published as a private claude.ai artifact (link in `CLAUDE.md`). **Update it
after every finished task.**

## Repos

| Repo | Visibility | What |
|---|---|---|
| [`minibank`](https://github.com/ducnghiaaa/minibank) | public | App code, specs, ADRs, prototype, this file, `CLAUDE.md`, skills |
| [`minibank-infra`](https://github.com/ducnghiaaa/minibank-infra) | public | Terraform, own `CLAUDE.md` |
| [`minibank-gitops`](https://github.com/ducnghiaaa/minibank-gitops) | private | Kubernetes manifests (empty) |

## Key facts

| Item | Value |
|---|---|
| Accounts | management `722433231642`, workload `701254204984` |
| Login | `aws sso login --sso-session minibank`; profiles `minibank-mgmt`, `minibank-workload` |
| Region | `ap-southeast-1` |
| Domain / zone | `andrewnguyen.cloud` / `Z0524474HG5AIAQLAIUA` (`prevent_destroy`) |
| VPC | `minibank-dev` `vpc-0fb4a5696500bc462`, `10.0.0.0/16`, 2 AZ |
| ECR | `minibank/{security,account,notification}` — immutable tags, scan on push |
| Messaging | SNS `minibank-dev-transactions` → SQS `minibank-dev-notification` (+ `-dlq`, 5 receives) |
| State bucket | `minibank-tfstate-701254204984` |
| Budget | $40/month |

## What is in `minibank-infra`

```
bootstrap/{management,workload}      S3 state buckets
management/organization              budget, Workloads OU, guardrail SCP
workload/dev/persistent/ci-oidc      GitHub OIDC provider, tf-plan, tf-apply
workload/dev/persistent/dns          hosted zone (imported)
workload/dev/persistent/network      VPC module 6.7.3, S3 gateway endpoint
workload/dev/persistent/registry     ECR repos + lifecycle (plain resources)
workload/dev/persistent/messaging    SNS → SQS + DLQ, queue policy Service sns + SourceArn
.github/workflows/terraform-pr.yml   fmt + plan on PR, one job per stack (matrix)
```

## What is in `minibank` (app)

```
compose.yaml                     postgres:16, account-migrate (one-shot), account, devtoken
cmd/devtoken/                    fake Cognito (RS256 access tokens) + fake security (ES256 txn tokens)
internal/platform/               logging, httpserver (graceful SIGTERM), auth (JWT), dbtest
services/account/
  migrations/                    0001 ledger tables, 0002 BANK_FUNDING + 5 merchants
  internal/ledger/               ledger.Lock, ledger.Post, CheckInvariants
  internal/accounts/             lazy opening, 10M gift, Luhn numbers, display names
  internal/transfers/            POST /transfers — spec §5.2, idempotency, jti, daily limit
  internal/txntoken/             ES256 transaction-token verifier
  internal/api/                  /healthz, /readyz, /accounts/me, /transfers
```

Run: `docker compose up --build`. DB tests: `docker compose up -d postgres` then
`TEST_DATABASE_URL='postgres://minibank:local-only@localhost:5432/account?sslmode=disable' go test -race ./...`.
Libraries: pgx v5.12.0, goose v3.29.0, golang-jwt v5.3.1, keyfunc v3.9.1.

## How we work

1. **Preview first:** every task gets a preview in the prototype; the user approves, then we build.
2. **Two tracks in parallel:** A = infra labs (user builds, Claude guides), B = app code (Claude writes, user reviews).
3. **Infra:** console lab → cleanup (Claude may delete lab resources when asked) → Terraform from scratch.
   `import` only when recreating would break things (`dns`).
4. Claude writes code; the user reviews, commits, pushes, merges. Small PRs.
5. Explanations and labs in Vietnamese; code, comments and commits in English.
6. Skills in `.claude/skills/`: `aws-lab`, `codify`, `handoff`, `adr`.

## Lessons learned

- **GitHub OIDC immutable subject:** `repo:ducnghiaaa@129480468/minibank-infra@1459049748:...`.
- **`DependencyViolation`:** delete in reverse build order (route table associations, detach IGW). Same in `down`.
- **VPC module trick:** `enable_nat_gateway = false` + `single_nat_gateway = true` → one private route table.
- **ECR multi-arch:** one tag = an image index + one image per CPU + attestations. Lifecycle rules
  never expire images an index still points to. A failed push can leave untagged attestations.
- **SQS vs RabbitMQ:** SNS ≈ exchange, SQS ≈ queue, delete = ack, visibility timeout instead of
  nack, DLQ after maxReceiveCount; DLQ retention must be longer than the source queue.
- **Queue policy:** console writes `Principal "*"` + `aws:SourceArn`; Terraform uses `Service sns` + SourceArn.
- **Parallel retries:** two requests waiting on the same `jti` must re-check idempotency after
  the first commits (READ COMMITTED) — found by a test, not by hand.

## Next steps

1. **Track A — Cognito lab, step 1:** create user pool (SPA app `minibank-lab-web`, sign-in by
   email, self-registration, required `name`, return URL `http://localhost:5173/callback`).
   Then: sign up a user → exchange code for tokens → decode the JWT and compare with devtoken → cleanup → Terraform `identity`.
   The token / code / PKCE flow was explained on 2026-11-22; offer to draw it in the prototype.
2. **Track B — `account` PR 5:** `GET /accounts/lookup` (masked name) + `GET /transactions` (cursor, filters).
3. Then `account` PR 6 (merchant payments), service `security` (PIN + KMS), service `notification`.

## Roadmap — left for phase 1 (~end of 12/2026)

**Persistent infra:** `identity` (Cognito) · `data` (KMS, Aurora, Secrets) · `edge` (ACM, S3 SPA,
CloudFront) · workload IAM roles · CI roles `lifecycle`, `app-ci`.
**Ephemeral infra:** `nat` · `eks` · `addons-iam` · `argocd-bootstrap`.
**GitOps:** add-ons, service manifests, DB bootstrap Job.
**App:** `account` PR 5–6 · `security` · `notification` · `frontend` · app CI.
**Infra CI / lifecycle:** tflint, trivy, Infracost, apply on merge, drift, `up`/`down`, auto-off at 01:00, Cost Anomaly Detection.
**Observability:** Grafana Cloud + 4 alerts.

## Open cleanups

- Spec §9.1 folder tree: add `dns/`, `registry/`, `messaging/`.
- `ci-oidc` and `dns` providers: add `env=dev` and `component=<folder>` tags.
- `dns/main.tf`: the `import` block is done; keep as record or remove.
- Default VPC (`172.31.0.0/16`) still exists in Singapore — decide keep or delete.
