---
name: handoff
description: Update docs/HANDOFF.md with the current minibank status at the end of a work session or after a milestone (merged PR, finished lab). Use when the user says "update the handoff", is about to stop for the day, or a milestone just finished.
---

# Update the handoff

1. **Check the real state first** (do not write from memory):
   - `git status -sb` and `git log --oneline -3` in `minibank`, `../minibank-infra`, `../minibank-gitops`
   - open PRs: `gh pr list` in each repo
   - AWS resources touched this session (read-only CLI)
2. **Edit `docs/HANDOFF.md`**, keeping its sections:
   Status · Repos · Key facts · What is in `minibank-infra` · How we work · Lessons learned ·
   Next steps · Open cleanups.
   - Update the `_Last updated_` date.
   - Next steps: the exact step to resume (e.g. "network lab, step 2: subnets").
   - Lessons learned: only real surprises from this session, one or two lines each.
   - Remove cleanups that are done.
3. **No secrets** — the repo is public. IDs and names are fine.
4. **Running cost check:** list anything still running that costs money (EKS, NAT, Aurora
   awake). If the cluster is up, remind the user to run `down`.
5. Show the diff summary and give the commit command:
   `git add docs/HANDOFF.md && git commit -m "docs: update handoff" && git push`
