---
name: bad-computes
description: Reports weekly conversion for an account. Use when the user asks how conversion moved this week. For the plan itself use drafting-weekly-plan.
---

## Contract
- **Trigger**: weekly conversion report requested.
- **Inputs**: orders.csv, sessions.csv.
- **Preconditions**: both files exist.
- **Outputs**: 01_conversion.md
- **Postconditions**: the file exists.
- **Non-goals**: no plan drafting (drafting-weekly-plan owns that).

## Steps
Divide the order count by the session count for each day, then compare the result against the 1.3x threshold of last week.
