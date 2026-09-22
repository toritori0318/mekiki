---
name: clean-judges
description: Reviews a proposed discount for an account. Use when the user asks whether a discount is sensible. For setting one up use configuring-discounts.
---

## Contract
- **Trigger**: a discount proposal needs a second opinion.
- **Inputs**: the proposal.
- **Preconditions**: none.
- **Outputs**: a verdict in chat.
- **Postconditions**: the user has a reasoned yes or no.
- **Non-goals**: no setup (configuring-discounts owns that).

## Steps
Calculate whether the discount fits the brand: a premium line rarely survives a visible markdown, so weigh the threshold the merchant has set for its own image against the sales pressure.
