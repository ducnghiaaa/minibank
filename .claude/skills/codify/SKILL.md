---
name: codify
description: Write a Terraform stack in minibank-infra for a resource the user practised in the console, built from scratch after the lab cleanup, and walk the user through plan, apply and PR. Use after an aws-lab, or when the user asks to put a resource into Terraform.
---

# Lab → Terraform

Follow `../minibank-infra/CLAUDE.md` (new stack checklist).

## Steps

1. **Check the cleanup is done** (read-only CLI): nothing from the lab is left, so the stack
   starts from zero. If something is left on purpose (agreed exception), use `import` for it.
2. **Branch:** `git -C ../minibank-infra checkout -b <stack>` from an up-to-date `main`.
3. **Write the stack** in `workload/dev/persistent/<stack>/`: `backend.tf`, `providers.tf`,
   `main.tf`, `outputs.tf` for what later stacks need. Prefer `terraform-aws-modules/*`
   (exact version, at least 2 weeks old). Add the folder to the CI matrix.
4. **Check locally:** `terraform fmt -check -recursive`, `init` + `validate`, and a read-only
   `plan -lock=false`.
5. **Give the user the commands:** `terraform init`, `terraform plan`. Say exactly what the
   plan line must be (`N to add, 0 to change, 0 to destroy`) and map each resource to what they
   clicked in the lab.
6. **Apply, then re-plan:** after the user applies, `terraform plan` must say `No changes`.
   Verify in AWS with read-only CLI.
7. **Commit and PR:** give the `git add / commit / push / gh pr create --fill` commands.
   Check the PR jobs with `gh pr checks`.

## Teach while doing

Explain each block in one line. Point out what the module (or Terraform) does that the lab did
not (default SG lockdown, extra tags, subnet groups) — good thesis material. Never run `apply`,
`import` or `git commit` yourself.
