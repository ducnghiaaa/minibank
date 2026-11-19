---
name: adr
description: Write an Architecture Decision Record in docs/adr/ for a non-obvious minibank choice, with options, cost numbers and the SAA-C03 domain. Use when a design or infra choice is made (e.g. NAT instance vs NAT Gateway) or the user asks for an ADR.
---

# Write an ADR

File: `docs/adr/NNNN-<short-kebab-title>.md` (next free number, 4 digits).
Language: Vietnamese (thesis material), service names and terms in English.

```markdown
# NNNN. <Title>

- **Ngày:** YYYY-MM-DD
- **Trạng thái:** Đề xuất | Đã chốt | Thay thế bởi NNNN
- **Domain SAA-C03:** <1–4> — <tên domain>

## Bối cảnh
What problem, which constraints (cost, security, effort, phase 1/2).

## Các phương án
| Phương án | Chi phí/tháng | Ưu | Nhược |
|---|---|---|---|

## Quyết định
What we chose, in one or two sentences.

## Lý do
Why — tied to the constraints above.

## Hệ quả
What we accept, what we must do because of it, when to revisit.
```

Rules:
- Cost numbers must come from AWS pricing for `ap-southeast-1`; say the date and that it
  should be re-checked with the Pricing Calculator.
- Mark who decided: **[user]** or **[claude]**, like the specs.
- Keep it to one page. Link the spec section it belongs to.
