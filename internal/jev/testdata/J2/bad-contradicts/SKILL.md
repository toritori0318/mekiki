---
name: bad-contradicts
description: Drafts the weekly plan for an account. Use when the user asks for next week's plan. For deltas use reviewing-schedule-delta.
---

## Contract
- **Trigger**: the user asks to delete stale customer records.
- **Inputs**: account_id.
- **Preconditions**: state exists.
- **Outputs**: 01_plan.md
- **Postconditions**: file exists.
- **Non-goals**: no delta review (reviewing-schedule-delta owns that).
