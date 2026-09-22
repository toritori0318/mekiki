---
name: auditing-skill-corpus
description: Audits a corpus of Agent Skills with the mekiki CLI and turns its findings into repairs. Use this when the user asks to lint, audit or mechanically check their skills, when a skill change needs a gate before review, or when they say 「スキル資産を監査して」「mekiki でチェックして」. A first-ever run is getting-started-with-mekiki's job; judging prose is a review skill's; creating one is skill-creator's.
---

## Contract
- **Trigger**: the user asks for a mechanical audit of a skill tree, or for a change to skills to be checked before it goes to review.
- **Inputs**: required: the path to the skill tree (a plugin tree, a `skills/` directory, or a single skill directory). optional: a base revision to compare against, and the org's `config.json`.
- **Preconditions**: `mekiki version` succeeds. If it does not, install it: `brew install toritori0318/tap/mekiki`, or `go install github.com/toritori0318/mekiki@latest` with `$(go env GOPATH)/bin` on PATH.
- **Outputs**: `out/<corpus>/auditing-skill-corpus/{YYYYMMDD}_audit/01_findings.json` and `02_repair-plan.md`.
- **Postconditions**: `01_findings.json` exists, and every error in it appears in the repair plan with either a concrete edit or a question that only the user can answer.
- **Non-goals**: does not handle the very first contact — installing the binary, discovering which corpus the user even means, the beginner-terms explanation of a first run (that is `getting-started-with-mekiki`); does not judge how a skill is written — its prose, its structure, whether an agent will misread it (that is `reviewing-skills`); does not create a skill (`mekiki new`, `skill-creator`); does not decide whether a skill should exist at all (`skill-evaluator`); does not run evals (`skill-creator` owns that workflow).

## Steps

### 1. Establish the baseline before reading a single finding

```bash
mekiki lint path/to/skills
```

If the first line on stderr is a note that there is no baseline, stop and settle that first.
Without one, **every skill counts as pre-existing and the strict half of the rules can only
ever warn** — so a corpus with no baseline never holds a newly added skill to the higher bar.

```bash
mekiki lint path/to/skills --update-baseline   # freeze today's inventory as pre-existing
```

Say what this does before running it: from then on the same defect is a warning on an old
skill and an error on a new one. `baseline.json` is an inventory of one environment, so it is
generated where it is used rather than carried between machines.

### 2. Pick the command that matches the question being asked

| The question | The command |
|---|---|
| **Lint this pull request / this diff.** What shape are the skills it touched in? | `mekiki lint PATH --changed` |
| Does this whole tree have errors right now? | `mekiki lint PATH --severity error` |
| Did this change make the corpus worse than it was? | two `--format json` snapshots, then `mekiki diff base.json head.json` |
| What does the whole estate look like? | `mekiki atlas PATH --out skill-atlas.html` |
| What did this change do to the estate? | `mekiki atlas PATH --base base.json` |

**"lint this PR", "check this diff", "スキルの差分を見て" is the first row, not the third.**
The first row answers *what state the touched skills are in*; the third answers *whether the
corpus regressed*. Asking `diff` the first question is how a review ends up with "no change"
while the skills the change edited are full of errors — `diff` compares two sets of findings,
so a pull request that edits a skill without altering which rules fire is silent by design.

```bash
mekiki lint path/to/skills --changed                    # against origin/HEAD
mekiki lint path/to/skills --changed --base release-1   # against anything else
```

`--changed` still evaluates the whole corpus — the cross-cutting rules cannot see a
duplicated reference or a broken delegation otherwise — and narrows only the report, to the
skills the change touched plus any skill those touched skills broke. The exit code follows
the narrowed report, so an inherited backlog never fails the review.

Reach for `diff` when the question really is about regression, which on an inherited tree is
the only fair gate: a plain lint fails every change until the whole backlog is cleared. It
exits non-zero **only when the change added an error**:

```bash
git worktree add ../base <base-revision>
mekiki lint ../base/path/to/skills --format json > base.json
mekiki lint path/to/skills --format json > head.json
mekiki diff base.json head.json
```

For a review a person will read, add `--format sarif` to either command and the findings land
on the lines they refer to rather than in a job log.

### 3. Sort the output before touching anything

- **errors** — these are the work. Each one either gets an edit or gets a question.
- **warnings** — these are for a human to triage and **never** fail a build. A rule is
  warn-severity precisely because it can be wrong; treating warnings as a to-do list is the
  most common way to make a corpus worse while making the output look better.
- **notes** (stderr) — the state of the configuration, not defects.

### 4. Repair at the layer the rule names

Read `references/rule-playbook.md` and work from it. Its organising idea, which decides most
cases on its own: **a finding names the layer that is wrong, and the repair belongs in that
layer.** Deleting the sentence that tripped a rule is almost never the repair — it removes the
evidence and leaves the defect.

Three things are decisions rather than edits, and belong to the user:

- a guard skill's name, when the org has not configured one — **never invent it**, because a
  declaration naming a skill that does not exist protects nothing
- whether a duplicated reference file is deliberate fan-out or drift
- whether a confirmation step is genuinely required before an outward-facing action

Ask about these; do not decide them.

### 5. Leave the artifacts behind

Write the raw snapshot to `01_findings.json` and the plan to `02_repair-plan.md`, one row per
error: rule, skill, what will change, and — for the three decisions above — the question and
who has to answer it. An audit whose result exists only in the conversation cannot be
re-checked.

## Gotchas

- **A suppression without a stated reason silences nothing.** The pattern requires the reason,
  so `<!-- mekiki: disable L6 -->` is inert while
  `<!-- mekiki: disable L6 -- the arithmetic lives in scripts/calc.py -->` works. This is by
  design: the reason is what makes the exception reviewable.
- **`--severity error` filters the display, not the exit code. `--changed` narrows both.**
  The asymmetry is deliberate: a severity filter must never turn a red build green, whereas
  narrowing to the change is the whole point of narrowing — a pull request should not fail on
  an error it neither introduced nor touched.
- **`--changed` reads git, so it needs a revision that exists.** The default base is
  `origin/HEAD`; a repository with no remote has none, and the run stops with a message
  naming `--base` rather than guessing. Uncommitted work counts as part of the change.
- **Read the notes `--changed` prints before reading the findings.** "no skill was touched"
  means the change never reached a skill, not that the corpus is clean; a skill reported as
  no longer present was deleted by this change, which is the most consequential thing a
  skills pull request can do.
- **Both snapshots in a `diff` must come from the same mekiki version and the same
  `config.json`.** A different rule set on either side makes the delta meaningless.
- **The delta is keyed by rule and skill, not by file and line.** So fixing one finding and
  introducing another of the same rule in the same skill nets to no change.
- **Rules that depend on org-specific names stay silent when nothing is configured.** A clean
  run on a corpus with no `config.json` does not mean the guards are in place; it means mekiki
  was never told what they are called.
- **The tree being inspected is only ever read.** Nothing mekiki writes lands inside it, so any
  repair is an edit you make deliberately.
