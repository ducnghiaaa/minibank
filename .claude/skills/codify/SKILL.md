---
name: codify
description: Turn AWS resources the user built in the console into a Terraform stack in minibank-infra, using import blocks, and walk the user through a clean plan, apply and PR. Use after an aws-lab, or when the user asks to import / bring a resource under Terraform.
---

# Console → Terraform

Follow `../minibank-infra/CLAUDE.md` (new stack checklist).

## Steps

1. **Collect the real state** with read-only CLI: every ID and every setting, including
   defaults (tags, comments, flags). This is what the code must match.
2. **Branch:** `git -C ../minibank-infra checkout -b <stack>` from an up-to-date `main`.
3. **Write the stack** in `workload/dev/persistent/<stack>/`: `backend.tf`, `providers.tf`,
   `main.tf` (with one `import { to = ..., id = ... }` per resource), `outputs.tf` for what
   later stacks need. Add the folder to the CI matrix.
4. **Check locally:** `terraform fmt -check -recursive`, `init -backend=false` + `validate`.
5. **Give the user the commands:** `terraform init`, `terraform plan`. Say exactly what the
   plan line must be (`N to import, 0 to add, X to change, 0 to destroy`) and what each
   expected change is (usually tags only).
6. **Read the plan with them** (or run `plan -lock=false` yourself). Stop on any `add`,
   `destroy` or `forces replacement`.
7. **Apply, then re-plan:** after the user applies, `terraform plan` must say `No changes`.
8. **Commit and PR:** give the `git add / commit / push / gh pr create --fill` commands.
   Check the PR jobs with `gh pr checks`.

## Teach while doing

Explain each block in one line (what Terraform does with it). Point out console-vs-Terraform
differences — they are good thesis material. Never run `apply`, `import` or `git commit`
yourself.
