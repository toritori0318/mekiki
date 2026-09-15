# Usage

The task-oriented companion to the [README](../README.md): how to run each command, how to
put mekiki in front of a pull request, and what to configure. The precise rule definitions
live in [DESIGN.md](DESIGN.md); the conventions themselves in
[SKILL_PROTOCOL.md](../SKILL_PROTOCOL.md).

日本語版: **[USAGE.ja.md](USAGE.ja.md)**

## Checking a corpus

```bash
mekiki lint path/to/skills                     # human-readable
mekiki lint path/to/skills --format json       # machine-readable snapshot
mekiki lint path/to/skills --severity error    # show errors only
```

A PATH may be a plugin tree or a single skill directory, and several may be given. With no
PATH, `MEKIKI_TARGET` is consulted; if that is unset it is an error rather than a guess.

Exit codes: **0** no errors, **1** errors present, **2** usage or runtime failure.
`--severity` filters the *display* only — it never changes the exit code, so a filtered run
cannot accidentally turn a red build green. Warnings never fail a build: a rule that can
produce false positives belongs in review rather than in a merge block.

## Checking a pull request

`--changed` narrows the report to the skills the change touched, which is what "lint this
pull request" asks for:

```bash
mekiki lint path/to/skills --changed                    # against origin/HEAD
mekiki lint path/to/skills --changed --base release-1   # against any other revision
```

The corpus is still discovered and every rule still evaluated in full: the cross-cutting
rules cannot see a duplicated reference or a broken delegation from one skill alone. Only the
report is narrowed, and a finding survives the narrowing when it belongs to a touched skill
**or when its message names one** — so deleting a skill still surfaces the flow it broke in a
skill this change never opened.

Unlike `--severity`, `--changed` narrows the exit code too. The asymmetry is deliberate: a
severity filter must never turn a red build green, whereas failing a pull request on an error
it neither introduced nor touched is the behaviour scoping exists to remove.

Read the notes before the findings. An empty report under a note saying no skill was touched
means the change never reached a skill, not that the corpus is clean; a skill reported as no
longer present was deleted by this change.

Uncommitted and untracked work counts as part of the change, so the same command answers for
a branch you have not pushed. A repository with no remote has no `origin/HEAD`, and the run
stops with a message naming `--base` rather than guessing.

## Gating a change in CI

Two gates that answer different questions.

**Does the corpus have errors?**

```bash
mekiki lint path/to/skills --format json --severity error   # exit 1 when errors exist
```

**Did this change make it worse?** This is a different question from the one above: `lint
--changed` reports the state of the skills a change touched, while `diff` reports whether the
corpus regressed. A pull request that edits a skill without altering which rules fire is a
`no change` under `diff` and can still be full of findings under `--changed`. On an inherited
corpus regression is the only answerable question: a plain gate fails every pull request until the whole backlog is cleared.
`mekiki diff` compares two `--format json` snapshots — it is a diff of *findings*, not of
files — and exits 1 **only when the change added an error**. An added warning passes and
stays in the review.

```bash
mekiki lint base/skills --format json > base.json
mekiki lint head/skills --format json > head.json
mekiki diff base.json head.json
```

```
added    warn  L6   acme-toolkit:reviewing-report  deterministic logic ("Calculate") is described in prose but there is no scripts/
resolved error L8   acme-toolkit:publishing-deck   external publication wording ("Publish") is present but Contract Preconditions states no human confirmation step

added 1 (0 errors), resolved 1
```

In a pull request the two snapshots are the same repository at two commits:

```yaml
# .github/workflows/skills.yml
on: pull_request
jobs:
  skills:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }          # the base commit has to be reachable
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: |
          go install github.com/toritori0318/mekiki@latest
          echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"
      - name: Lint what this pull request touched
        run: mekiki lint path/to/skills --changed --base origin/${{ github.base_ref }}
      - name: Snapshot base and head
        run: |
          git worktree add ../base ${{ github.event.pull_request.base.sha }}
          mekiki lint ../base/path/to/skills --format json > base.json
          mekiki lint path/to/skills --format json > head.json
      - name: Fail only on what this pull request added
        run: mekiki diff base.json head.json
```

`mekiki diff --format json` returns `added`, `resolved` and a summary, which is what to
post as a pull-request comment.

**Annotating the diff on the changed lines.** `--format sarif` emits the same findings as a
SARIF 2.1.0 log, which GitHub renders as inline annotations in the pull request instead of
text in a job log. On `diff` it carries **only the added findings**, which is the whole point:
uploading a full lint of an inherited corpus would annotate the backlog on every review.

```yaml
      - name: Annotate what this pull request added
        run: mekiki diff base.json head.json --format sarif > mekiki.sarif
        continue-on-error: true            # keep the upload step reachable when the gate fails
      - uses: github/codeql-action/upload-sarif@v3
        with: { sarif_file: mekiki.sarif }
```

Paths in the log are relative to the working directory, so run mekiki from the repository
root or the annotations will not land on any file. `mekiki lint --format sarif` exists too,
for the case where annotating the whole corpus really is what you want.

The delta is keyed by rule and skill, deliberately not by file and line. That is what lets
the base tree sit at a different path, and what keeps an edit that shifts a line from
reading as one fix plus one regression. Two consequences worth knowing: fixing one finding
and introducing another of the same rule in the same skill nets to no change, and when a
skill holds several findings of one rule the count is exact but the line quoted is the
first of them, not necessarily the one that appeared.

Both snapshots must come from the same mekiki version and the same `config.json`, and
neither may be taken with `--severity error` — the filter drops warnings from the snapshot,
so the other side's warnings would all read as added or resolved. A different rule set on
either side makes the delta meaningless. The same applies to a snapshot handed to
`atlas --base`.

## Adopting on an existing corpus

Adoption does not mean fixing everything first.

```bash
# Freeze today's skills as pre-existing, so error severity applies only to new ones
mekiki lint path/to/skills --update-baseline
```

`baseline.json` records the skill inventory of whatever corpus you point at, so it is
generated per environment and not shared. `--baseline` defaults to `baseline.json`; absent
means every skill counts as pre-existing.

`--update-baseline` and `diff` do different jobs and compose: the baseline decides
*severity* (a finding on a pre-existing skill is a warning, the same finding on a new skill
is an error), while `diff` decides *what this change is responsible for*.

## Scaffolding a new skill

```bash
mekiki new drafting-weekly-plan --out path/to/skills
```

```
created: path/to/skills/drafting-weekly-plan
Before you fill this in: a new skill is a last resort. If an existing skill's Gotchas or
description, a line in the agent instructions, or a paths/hooks setting would do, delete
this and grow the existing skill instead — the always-on catalog cost scales with the
number of skills.
next: fill in the 11 [TODO] markers in SKILL.md (the description drives activation — see
      the three-part order in the template)
      then the two eval files: evals.json (output quality) and eval_queries.json (trigger accuracy)
```

`--type action|knowledge|util` and `--risk billing|write|browser|publish` shape the
skeleton. A risky skill gets a `## Guard` section that names the guard from your
`config.json` and tells the model to invoke it — the `requires:` line alone starts
nothing — plus `disable-model-invocation` where the side effects are irreversible. With no
guard configured for that tier the scaffold leaves a marked TODO rather than inventing a
name that would protect nothing.

## Rendering the Atlas

```bash
mekiki atlas path/to/skills --out skill-atlas.html
```

One self-contained HTML file: no server, no network access, nothing to install on the
reader's side. The default output path is the working directory on purpose — the page
embeds every description in the corpus, so it must never default into a tracked location.
`.gitignore` in this repository already excludes `skill-atlas.html` for the same reason.

The three views are described in the [README](../README.md#the-atlas).

**Reviewing one change.** Give the Atlas the base snapshot and the page marks the delta:
a `new` badge on skills the change introduced, `+N E` for errors it added, and a header
line with the totals. The Catalog gains a sortable `Δ err` column.

```bash
mekiki lint ../base/path/to/skills --format json > base.json
mekiki atlas path/to/skills --base base.json --out skill-atlas.html
```

Attach that file to the pull request and a reviewer sees the change in the context of the
whole estate, rather than a list of findings with no map. Without `--base` the page
carries no delta data at all, so the default output is unchanged.

Tiers are deliberately **not** compared. A snapshot does not record them, and recomputing
the base tree's tiers would mean linting a corpus the page was never given.

## Driving the audit from an agent

```
/plugin marketplace add toritori0318/mekiki
/plugin install mekiki@mekiki
```

The repository doubles as a Claude Code plugin marketplace, so the two bundled skills
install as a managed plugin; copying by hand (`cp -r skills/* ~/.claude/skills/`) works too. `getting-started-with-mekiki` covers the first
contact — install, find the corpus (asking between candidates rather than guessing), first
lint, first Atlas, and a plain-terms reading of the summary — and deliberately stops short
of baselines and repairs. `auditing-skill-corpus` is the operating workflow, and exists for
the part of this document a reader skips: the **order**.
It settles the baseline before reading a finding, gates a change on its delta rather than the
inherited backlog, repairs at the layer a rule names instead of deleting the sentence that
tripped it, and asks rather than invents when a guard name is missing. Per-rule repair
guidance is in its `references/rule-playbook.md`, which is the one piece of prose that says
how to *fix* a finding rather than what the rule means.

It claims the mechanical audit only. Judgement about how a skill is written — its prose, its
structure, whether an agent will misread it — belongs to a review skill, and its Non-goals
says so, so the two do not compete for activation.

## Configuration

`config.json` and `baseline.json` are both optional; mekiki works without either.

```bash
cp config.example.json config.json
```

`config.json` holds four things:

- **`guards`** — the guard skill names to demand for billable, destructive and
  browser-automating skills. These are org-specific, so they are not compiled in: a
  hardcoded name would make every other organisation's CI demand a skill that does not
  exist there. A risk tier with no configured guard is simply not checked.
- **`patterns`** — the detection regexps behind the heuristic rules. The built-in defaults
  are **bilingual (English and Japanese)**, so most users need nothing here. An override
  replaces a key entirely; an invalid regexp produces a note and falls back for that key
  only.
- **`profile`** — which runtime the corpus targets. `claude-code` is the default and the
  behaviour mekiki has always had: it knows the extension keys and the 1,536-character
  skill-listing cap. `generic` checks the Agent Skills specification only, so Claude Code
  extensions such as `when_to_use` are reported as non-standard (L12) and no listing cap
  applies (L2). An unknown name produces a note and falls back to `claude-code`.
- **`limits`** — the numeric thresholds (`listing_cap`, `max_body_lines`,
  `max_body_tokens`). Omit them to take the profile's values; an entry here wins, and `0`
  turns that check off. The body-size limits come from the official guidance rather than any
  one runtime, so both profiles carry the same values.

## Suppressing a false positive

A suppression without a stated reason is invalid by design — the reason is what makes the
exception reviewable.

```markdown
<!-- mekiki: disable L6 -- explains judgement criteria; the arithmetic lives in scripts/calc.py -->
```

## Scope and assumptions

- **The inspected tree is read-only.** `lint` and `atlas` never write to it; generated
  files land where you ask for them.
- mekiki judges **your own** skills. Vetting third-party skills for malware or prompt
  injection is a different problem and out of scope.
- Detection patterns ship bilingual; other languages are one `patterns` override away.
  Structural rules are language-independent.
- Examples in the conventions come from the corpus mekiki was first built against. Read
  them as shapes to recognise in your own estate, not as literal skill names.
