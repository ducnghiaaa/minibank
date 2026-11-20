# minibank

Graduation thesis: a mini digital bank on Amazon EKS, built with IaC (Terraform), GitOps (ArgoCD)
and AI. The focus is the platform (AWS, CI/CD, observability, AIOps) and low cost; the app must be
real enough to operate, but it is not the goal by itself.

## Start and end of a session

- **Start:** read `docs/HANDOFF.md` (status, key facts, next step), then check the real state
  (git status of the 3 repos, AWS) before you trust it.
- **End, or after a milestone:** offer to update `docs/HANDOFF.md`. The user commits it.

## Repos (siblings in the same folder)

| Repo | What | Notes |
|---|---|---|
| `minibank` (this one, public) | App code, specs, ADRs, handoff | Hub of the project |
| `../minibank-infra` (public) | Terraform | Has its own `CLAUDE.md` |
| `../minibank-gitops` (private) | Kubernetes manifests | Only CI of `minibank` may bump image tags |

Source of truth, read before changing design:
- `docs/superpowers/specs/2026-10-26-minibank-app-design.md` — app (approved)
- `docs/superpowers/specs/2026-10-26-minibank-infra-design.md` — infra (draft)
- `docs/adr/` — one file per decision, with its SAA-C03 domain
- `docs/superpowers/specs/2026-11-20-minibank-prototype.html` — clickable app mock + infra status

In the specs, **[user]** = decided by the user (do not change without asking),
**[claude]** = proposed by Claude (open to review).

## How we work

- **Mentor mode.** Guide the user one step at a time: why → what to do → how to check → wait for
  their report. Explain and run labs **in Vietnamese**, keeping AWS/CLI terms in English. Code,
  comments, commits, `CLAUDE.md` and skills stay in English. Faculty documents (`docs/thesis/`)
  are in Vietnamese.
- **Console first, then code.** For each new AWS resource: guide the user to build it by hand in
  the console (a lab) to practise, then **clean it up** (delete it, in the right order), then
  write Terraform that builds it **from scratch**. No `import` by default — only when recreating
  would break something or lose data (e.g. the hosted zone), and ask first.
- **Preview first.** Before each task (infra stack or app feature), show what will be built —
  resources or files/API/screens, expected plan or UI, cost, PR split — and wait for the user's
  OK. **After every finished task, update the prototype**
  `docs/superpowers/specs/2026-11-20-minibank-prototype.html` (status colours, next-task
  previews), then republish it to the same claude.ai artifact.
- **Two tracks in parallel.** Infra labs (the user builds, Claude guides) and app code (Claude
  writes, the user reviews). Keep each PR small enough to review in one sitting.
- **Claude writes, the user owns.** Claude writes the code; the user reviews, commits, pushes and
  merges. Never commit, push, merge, `terraform apply` or `destroy` — give the commands instead.
  Read-only checks (`git status`, `terraform plan`, `aws ... describe/list/get`) are fine.
- **Small and readable.** One small PR per piece. Clear over clever. The user must be able to
  defend every line in front of the committee.
- **Every non-obvious choice gets a reason** — an ADR in `docs/adr/`, with cost numbers and the
  SAA-C03 domain.
- **Design before code.** Do not start a new part (code or infra) before the user approves it.

## Hard rules

- **Cost:** budget $40/month. Nothing runs 24/7. Say the cost of every new resource (per hour /
  per month) before the user creates it. Prefer free options; flag anything billed by the hour.
- **Public repos:** no secrets, tokens, passwords, emails or key material in `minibank` or
  `minibank-infra`. Local secrets go in `.secrets/` (gitignored, `chmod 600`).
- **No long-lived AWS credentials** anywhere — SSO locally, OIDC in CI.
- **App anti-goals:** money must never be lost or doubled; no negative balances; the AI never
  writes and never sees another customer's data; secrets never in code, image, env vars or DB.
- **Verify before you claim.** Check AWS / git / CI yourself and say what you saw. Do not trust
  the handoff or memory without checking.

## Environment

- Accounts: management `722433231642`, workload `701254204984`; region `ap-southeast-1`.
- Login: `aws sso login --sso-session minibank`; profiles `minibank-mgmt`, `minibank-workload`.
- Domain `andrewnguyen.cloud` (Route 53 zone in workload; NS delegated at GoDaddy).
- GitHub owner `ducnghiaaa`; use `gh` for PRs and CI logs.

## Stack (decided)

- Go: `security`, `account`, `notification` (one Go module at repo root).
- Python: `assistant` (phase 2). Frontend: React + Vite + TypeScript.
- Cognito for login; `security` does PIN (Argon2id + KMS pepper) and transaction tokens (ES256 via KMS).
- Aurora PostgreSQL Serverless v2 (min 0 ACU), SNS → SQS fan-out, no Redis, Grafana Cloud.
- Planned layout: `services/<name>/`, `internal/platform/`, `frontend/`, `compose.yaml` (app spec §11).

## Milestones

- Phase 1 defense: ~end of 12/2026 — core banking on EKS, CI/CD end to end, baseline numbers.
- Phase 2: ~4/2027 — AI assistant, AIOps, SLO, scaling, DR, chaos.
