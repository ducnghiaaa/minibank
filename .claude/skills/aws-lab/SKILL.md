---
name: aws-lab
description: Guide the user to build a new AWS resource by hand in the console, step by step, then clean it up, before it is written in Terraform. Use when starting any new minibank infra piece (VPC, subnets, KMS, Aurora, ...) or when the user says "lab".
---

# Console lab

The user builds it by hand first, to practise the console, then deletes it. Terraform then
builds it from scratch (see `codify`).

## Before step 1

1. Read the matching spec section in `docs/superpowers/specs/2026-10-26-minibank-infra-design.md`.
2. Check what already exists in AWS (read-only `describe`/`list`) so you do not repeat work.
3. Tell the user, in one short table: what we will build, the **cost** (per hour / per month,
   "free" if free), and the SAA-C03 topic it teaches.
4. Split the lab into small steps (one resource or one setting per step).

## Each step

Write it in plain English, in this shape:

- **Why:** one or two sentences — what this resource does in minibank.
- **What to do:** numbered clicks, with exact names and values (account, region, Name tag).
  Name resources `minibank-dev-<thing>` unless the spec says otherwise.
- **How to check:** one read-only CLI command the user runs, and what the output should show.
- **SAA tip** (optional): one exam-relevant fact.

Then **stop and wait** for the user's report. Verify it yourself with read-only CLI before
the next step. If something differs, explain why and how to fix it before moving on.

## Rules

- Never pick "wizard" options that build many things at once (e.g. "VPC and more"),
  unless the user asks.
- Do not let the user create anything billed per hour without saying so first.
- Note every console default that Terraform sets differently (comments, tags, flags) —
  `codify` needs this list.
- At the end: summarise what was built, then run a **cleanup** lab: delete everything in
  dependency order (explain why each thing must go before the next), and verify with CLI that
  nothing is left. Then offer `codify`.
- Exception: if recreating would break something or lose data (e.g. a hosted zone and its NS),
  do not clean up — ask the user, and `codify` imports it instead.
