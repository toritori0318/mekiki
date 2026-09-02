# SkillProtocol v1.0 — the conventions mekiki enforces

Written for the people who author and review skills. The precise mechanics live in
[docs/DESIGN.md](docs/DESIGN.md); what was adopted, rejected and why is in
[docs/DECISIONS.md](docs/DECISIONS.md). This document holds only what you consult daily.

日本語版: **[SKILL_PROTOCOL.ja.md](SKILL_PROTOCOL.ja.md)**

Examples and measured figures come from the corpus mekiki was first built against
(6 plugins, ~190 skills). Read the skill names as shapes to recognise in your own estate.

`mekiki lint` checks most of this, but **the enforcement level differs per rule**:
heuristics that can produce false positives are warn-only, pre-existing skills are
downgraded via the baseline, and some conventions cannot be judged mechanically at all.
The mapping is in [§Enforcement](#enforcement). Because only errors fail the exit code,
**a MUST violation that lands at warn passes the gate** — that part is on review.

---

## First principle

> **Put deterministic work in code, judgement in the prompt, and enforcement in hooks.
> Declare the specification; verify it with code.**

A `SKILL.md` is not compiled. A probabilistic model reads and interprets it. So a rule you
want followed either (a) states its intent in prose, or (b) is mechanically enforced through
`scripts/`, `hooks/` or frontmatter. What does not work is writing it vaguely in prose and
hoping. Measured evidence: a prose-only guard skill was invoked once in fourteen days.

## Layers

| Layer | Where | Responsibility |
|---|---|---|
| L0 | frontmatter `name` / `description` | Activation. Half of a skill's accuracy is decided here. |
| L1 | `SKILL.md` body | Workflow and judgement. Opens with the Contract. |
| L2 | `references/` | Knowledge loaded on demand. |
| L3 | `assets/` | Output templates the model can copy verbatim. |
| L4 | `scripts/` | Deterministic work plus tests. The other half of accuracy. |

Hooks live in two places: the plugin's `hooks/hooks.json` (active for the whole session),
and a skill's own `hooks:` frontmatter key (active only while that skill runs, cleaned up
on exit, all events supported). Put skill-specific guards in the latter so they travel with
the skill. mekiki does not inspect hook contents — see [§Safety](#safety-two-layers-must).

## Determinism boundary [MUST]

Arithmetic, threshold decisions, SQL generation, data shaping, reconciliation, outbound
payload transforms, sanitisation and schema validation **belong in `scripts/`, with tests
beside them**. Do not describe a deterministic algorithm in prose. Prose keeps judgement,
synthesis, drafting and hypotheses.

Detected by **L6**, which is warn-only because the detection is heuristic. CI can pass with
violations present, so review for it. Suppress false positives with a stated reason
(see [§Suppression](#suppression)).

### Script interfaces [MUST]

An agent reads stdout and stderr to decide what to do next. That is a different contract
from a human-facing CLI.

- **Never prompt interactively.** Agents run in non-interactive shells, so a TTY prompt
  hangs forever. Take input from flags, environment variables or stdin. A missing required
  argument should fail immediately with the valid options listed.
- **`--help` is the primary interface.** It is how an agent learns your script. Keep it
  short — help output costs context too — and document what each exit code means.
- **Errors say what happened, what was expected, and what to try.**
  `Error: --format must be one of: json, csv, table. Received: "xml"` is useful;
  `Error: invalid input` wastes a turn.
- **Structured data on stdout, diagnostics on stderr.** JSON, CSV or TSV. Do not make an
  agent parse a whitespace-aligned table.
- **Be idempotent** (assume retries: "create if absent" beats "create and fail"). Give
  destructive operations a `--dry-run` and an explicit confirmation flag.
- **Bound your output.** Harnesses truncate tool output somewhere around 10-30K characters.
  If output can grow, summarise by default and offer `--offset`, or require `--output`.
- **Make dependencies self-contained** (in Python, PEP 723 inline metadata with
  `uv run scripts/x.py`). Pin versions.
- **Mark deliberate shortcuts.** Record a known trade-off — a coarse lock, an O(n²) scan, a
  naive heuristic — as `// simplified: <ceiling>. Upgrade when: <condition>`. A suppression
  argues with the linter; this records your own design decision so it can be harvested
  later with `grep simplified:` instead of rotting silently.

## Before writing a skill [MUST]

**A new skill is a last resort.** The always-on catalogue cost (see
[§Context minimality](#context-minimality-must)) is paid every session regardless of whether a
skill fires, and it scales with skill count. So "should this exist at all" comes before
"is its content minimal". Work down this ladder and stop at the first rung that suffices:

1. **Would an edit to an existing skill do?** A correction the model needs is one line in
   that skill's Gotchas; an activation problem is a description fix. Grow, don't add.
2. **Would one or two lines of agent instructions do?** Facts and standing rules belong
   there. Conversely, when a section of those instructions has grown into a procedure, that
   is the signal to extract it into a skill.
3. **Would configuration do?** `paths` for activation control, `hooks` for enforcement,
   `allowed-tools` for permission. More reliable than prose, and it costs no context.
4. **Would a file in an existing skill's `references/` do?** New knowledge goes to the
   on-demand layer, not into a new skill.
5. Only when all of the above are "no", create it — with `mekiki new`, which reminds you of
   this ladder.

Once created, prove it beats not having it with the with/without comparison in
[§Evals](#evals-must). If activation could collide with a neighbouring skill, write the mutual
redirection into both descriptions and Non-goals.

Not mechanically checkable, so **unenforced** — make it the first question in review.

## Context minimality [MUST]

A skill should carry only the smallest set of high-signal information needed to do its job.
From the moment it loads, a `SKILL.md` competes for the same context window as the system
prompt and the conversation. Irrelevant content is not harmless padding: it **measurably
degrades accuracy**, to the point where a single distractor makes a model fail a problem it
would otherwise solve. Sources are in [docs/DECISIONS.md](docs/DECISIONS.md).

Every paragraph must survive these five tests:

1. **Already-known test.** Do not explain general knowledge, concepts, or the ordinary use
   of a tool. The model is already capable; write what it would otherwise get wrong.
2. **Deletion test.** If deleting the paragraph does not change behaviour, delete it.
   *Does this paragraph justify its token cost?*
3. **Action test.** Write what to do and how to decide. Do not narrate background, history
   or development notes — the *why of the project* belongs in commit messages and decision
   records. Curate representative cases instead of enumerating every edge case.
4. **Layer test.** Needed every run → body. Needed only on a branch → `references/`
   (loaded on demand). Deterministic → `scripts/` (executed, never loaded). The body should
   read like a table of contents plus a workflow. A large `references/` is harmless: it
   costs nothing until read.
5. **Duplication and contradiction test.** Activation conditions live in the description
   only — do not restate them in the body. Do not state the same fact twice. Do not leave
   time-bound wording ("currently", "the new approach") or contradictory old and new text.

**Size discipline.** The official guidance has two halves: **under 500 lines and under
5,000 tokens**. Line count alone misses the point on a CJK corpus — measured at ~76
characters per line, one corpus had exactly one skill over 500 lines while 93 exceeded
5,000 characters of body. **L19** therefore checks both, estimating tokens at 1.2 per CJK
rune. Size is a *proxy* for the real question, so it stays warn-only. Tests 1-5 cannot be
judged mechanically — that is review's job.

**The always-on catalogue cost.** `name` + `description` (+ `when_to_use`) are loaded for
**every** skill, every session, whether or not anything fires — tier 1 of progressive
disclosure, which the official guidance sizes at 50-100 tokens per skill. Unlike body size
this is unconditional and grows with the number of skills. Measured: ~42,250 tokens across
174 skills, averaging 243 each, two to three times the official expectation. Every word you
add to a description makes every session heavier, so keep only what earns activation
accuracy (weigh that trade-off with the activation measurement in [§Evals](#evals-must)).
Setting `disable-model-invocation: true` on a human-only skill removes it from the catalogue
entirely, and with it this cost.

One clarification on test 3: what to leave out is *project history*. The **reason behind an
instruction** should be written — the official guidance is explicit that "explaining *why*
can be more effective than rigid directives", because a model that understands the purpose
makes better context-dependent decisions. Reasoned instructions are followed more reliably
than shouted ones.

## Directories [MUST]

- References go in `references/` (plural), code in `scripts/`, templates in `assets/`.
  `reference/`, `bin/` and `assets/scripts/` are not acceptable.
- Do not commit build artifacts (`*.zip` and friends).

## Frontmatter

**Official keys** — the Agent Skills specification plus the Claude Code extensions. The
runtime interprets these.

```yaml
name: <required. 1-64 chars, lowercase alphanumerics and hyphens, no leading/trailing or
       consecutive hyphens, equal to the directory name>
description: <required. Max 1024 chars (this protocol asks for 150-400). See §L0>
when_to_use: <extra activation context. Concatenated onto description; the listing shows
              the combination up to 1,536 characters>
paths: ['src/**/*.ts']  # auto-activate only when working on matching files
allowed-tools: <tools usable without a permission prompt. A grant, not a restriction>
disallowed-tools: <tools removed from the pool while this skill is active>
disable-model-invocation: true  # required for side-effecting skills; stops autonomous use
user-invocable: false   # required for internal skills. Only hides it from the / menu
context: fork           # run in a subagent to isolate context
agent: <subagent type when context: fork>
background: false       # await the forked result in the invoking turn
model: <model override while this skill is active>
effort: low             # effort override while this skill is active
hooks: {...}            # hooks scoped to this skill's lifetime
arguments: [issue]      # named positional arguments; $issue / $ARGUMENTS expand in the body
argument-hint: '[issue-number]'
shell: bash             # shell for inline commands in the body
license: <license name or bundled file>
compatibility: <max 500 chars. Environment requirements. Most skills need none>
metadata: {author: <org>, version: "1.0"}
```

**Protocol-specific keys** — the runtime ignores these; only mekiki and audits read them.

```yaml
status: active          # active | deprecated | experimental. Lifecycle goes here, not in description
canonical: <plugin:skill>     # points at the source of truth when duplicates exist
duplicate_of: <plugin:skill>  # placed on the intentional copy
requires: [<guard-skill>]     # required per risk tier. A declaration starts nothing
depends_on: [<artifact>]      # when a prior skill's output is an input
```

- [MUST] The authoritative place for activation conditions is `description`. `when_to_use`
  is an official key and does influence activation in Claude Code, but **other clients do
  not read it**, so portable skills put it in the description. Past 1,536 combined
  characters the listing truncates, and the excess never reaches the activation decision
  (**L2**).
- [MUST] Keep lifecycle wording and development notes out of the description (**L3**).
- Protocol keys do not take effect by being declared. Behaviour comes from the body,
  `scripts/` and hooks.
- `user-invocable: false` **only hides the skill from the `/` menu**; it does not prevent
  invocation through the Skill tool. To stop the model choosing it, use
  `disable-model-invocation: true`. These are different things.
- `allowed-tools` **grants** permission, it does not restrict. A skill can hand itself
  broad tool access, so read a third-party skill before you trust it.

## L0: naming and description

**Naming [MUST]** — the official constraints, checked by **L1**: 1-64 characters, lowercase
alphanumerics and hyphens only, no leading, trailing or consecutive hyphens, equal to the
directory name, and no reserved word (`anthropic`, `claude`).

**Naming shape [SHOULD]** — not mechanically checked, for the reason below:

| Kind | Suggested shape | Example |
|---|---|---|
| Action | `verb-ing-object` | `drafting-weekly-plan` |
| Knowledge | `knowledge-<domain>` | `knowledge-shared-master` |
| Shared utility | noun, up to three words | `billing-guard` |

These three shapes were enforced once. Measured, it collapsed: all 25 findings were
perfectly valid official names, 62% of skills slipped through the "noun, three words"
catch-all so the gerund requirement never actually bound, and the problem worth catching —
a name/directory mismatch — occurred zero times. Renaming also breaks existing references.
**A name is not the primary driver of activation** (the description is), so no enforcement
budget is spent here.

**description [MUST]** — 150-400 characters, in this order:

1. What it does, in the third person.
2. When it activates, listing concrete symptom and request wording.
3. When it does *not* activate, redirecting to the neighbouring skill.

**paths [SHOULD]** — when a skill only makes sense for particular file types or directories
(a language's coding conventions, a directory's operational runbook), restrict activation
with a `paths` glob. It is more reliable than trying to narrow activation through
description wording, and it mechanically prevents over-firing on unrelated tasks.

## L1: the Contract [MUST]

The first `##` heading in the body is `## Contract`:

```markdown
## Contract
- **Trigger**: one sentence, consistent with the description
- **Inputs**: required / optional, and how each is obtained
- **Preconditions**: what must be true before running (a verification command if possible)
- **Outputs**: generated paths and formats
- **Postconditions**: how completion is judged (a scripts gate if possible)
- **Non-goals**: what this does not do, and which skill or stage owns it instead
```

**Non-goals matters most** — it is what prevents scope creep and re-implementation of
upstream steps. Prefer *executable gates* over sentences for Pre/Postconditions: a
read-only verification script that exits non-zero until the work is genuinely done.

## L1: body structure [SHOULD]

Patterns that follow the Contract. Use the ones that fit; you do not need all of them.

### Gotchas — environment-specific facts that defy assumption

The official guidance calls this "the highest-value content in many skills". Not general
advice ("handle errors appropriately") but concrete corrections to mistakes the model will
otherwise make:

```markdown
## Gotchas
- The `users` table uses soft deletes. A query without `WHERE deleted_at IS NULL`
  includes deactivated accounts.
- The same value is `user_id` in the database, `uid` in the auth service and
  `accountId` in the billing API.
- `/health` returns 200 while the database is down. Use `/ready` for real liveness.
```

- **Keep these in the body, not in `references/`.** If the model does not recognise that it
  has hit the situation, it never knows to load the reference file.
- **Every time you correct the model, add a line here.** It is the shortest path from a
  mistake to a durable fix.

### Other patterns

| Pattern | When | Point |
|---|---|---|
| Output template | You need a specific format | Paste the skeleton rather than describing it — models pattern-match well against concrete structure. Long or conditional templates go to `assets/`. |
| Checklist | Multi-step work with dependencies or gates | `- [ ]` lets the model track progress and stops steps being skipped. |
| Validation loop | You want higher output quality | State it explicitly: do the work, run the validator, fix, repeat until it passes. |
| plan-validate-execute | Destructive or bulk operations | Write an intermediate plan to a file, have a script check it against the source of truth, then execute. Include the *available options* in the validation error so the model can self-correct. |
| atomic deliver | Updating an existing artifact | Write to a temporary path, run the gate, then swap. **Never overwrite a good artifact with output that failed validation.** |

Match prescriptiveness to fragility. For operations that are fragile or order-dependent,
say "run exactly this command; do not modify it". For judgement with several valid answers,
give the direction and the reason. Offer **one default** rather than a menu, with
alternatives mentioned briefly.

## Orchestration [MUST]

A skill that calls other skills in sequence declares the call order and the inter-phase
inputs and outputs in a **machine-readable `flow.json`**. Do not rely on a prose data-flow
diagram or a "Phase Registry" table alone: a human can read those, but nothing can verify
or draw them.

```json
{
  "flow": [
    {"phase": 1, "skill": "collecting-context",
     "inputs": ["target_id"], "outputs": ["01_context.json"]},
    {"phase": 2, "skill": "drafting-strategy",
     "inputs": ["01_context.json"], "outputs": ["02_strategy.json"]}
  ]
}
```

- [MUST] Each element has `phase` (int), `skill` (str), `inputs` and `outputs` (lists).
  Phases are unique and ascending (**L15**, error).
- [MUST] `flow[].skill` names an existing skill (**L16**, error).
- [MUST] If an input matches another phase's output, the producing phase comes first
  (**L17**, warn — it passes the gate, so review for it). External inputs that no phase
  produces are listed as-is.
- **The existence of `flow.json` is unenforced**, because "is this an orchestrator" cannot
  be judged mechanically. Check for it when reviewing a new orchestrator.
- With the declaration, `mekiki atlas` draws the flow exactly. Without it, the page falls
  back to *inferring* one from the body and labels it as inferred.

## Artifacts [MUST]

- Write to files. Do not finish by pasting into the conversation.
- Path: `<base>/<target>/<skill>/{YYYYMMDD}_<slug>/NN_<artifact>.<ext>`. Directory names
  are ASCII only.
- Numbers have a single source of truth in one file; downstream artifacts reference it
  (`_ref`) rather than recomputing or re-typing.
- [SHOULD] **Structured artifacts — plans, diagnoses, schedules — should be authored as an
  intermediate representation.** Have the model write schema-checkable JSON and let a script
  validate and transform it; the human-readable Markdown is a generated view (the same shape
  as `tables/` → `views/` for master data, or `flow.json` → the Atlas). This makes the
  judgement itself the subject of mechanical verification and eliminates formatting drift,
  hand-copied numbers and artifacts only a human can check. Do not render a view from an IR
  that failed validation.

## Data format selection

| How it is used | Format |
|---|---|
| Prose, free-form reading | Markdown |
| Structured, diff-sensitive | YAML |
| High-volume append-only history | JSONL |
| Executable queries | `.sql` |
| Large relational joins and aggregation | SQLite plus a script (migrate when it breaks, not before) |

- [MUST] Do not model relations as a Markdown table for the model to join. Express
  relations by ID.
- [SHOULD] Separate an immutable snapshot (baseline) from a mutable view (current).

## Single source of truth [MUST]

Shared knowledge, master data, common rules and shared scripts exist in exactly one place.
Physical copies are not allowed. There are two exceptions, and both require the source and
the regeneration method to be machine-readable:

1. A `canonical` / `duplicate_of` frontmatter declaration.
2. **A burned-in copy carrying a generated banner** — a leading comment with an
   `AUTO-GENERATED` style marker plus the regeneration or sync command (for example a
   fan-out from `master/` into each skill's `references/master/`). Operate it together with
   drift detection in CI.

An undeclared identical copy is an **L7** error; a declared or bannered one is a warning.

## Master data format [MUST] (conventions for data an AI reads)

When distributing master data — catalogues, dictionaries, mapping tables — as Markdown:

1. **Provenance banner.** The leading comment states that the file is generated, its
   upstream source, and the regeneration command.
2. **Retirement status lives in the file itself.** "This file is superseded, do not use"
   belongs in line 1 of *that file*, not in a README. A README does not travel with a
   fan-out, so the warning vanishes in the copy.
3. **Closed-world declaration and a count.** State up front: "N entries total; anything not
   listed here does not exist — do not invent one." This suppresses hallucination and lets
   the model self-check completeness.
4. **IDs are ASCII slugs, separate from display names.** `community` is fine; an ID that
   doubles as a localised display name breaks every reference the moment it is renamed.
   Express relations by ID.
5. **Do not flatten structure.** Keep checklists and numbered procedures as line breaks and
   bullets (aim for under 500 characters per line). Cramming thousands of characters onto one
   line breaks line-oriented tooling — partial reads, `grep -n`, `diff` — and heading-based
   chunking.
6. **Summary then detail, with reading instructions.** Open with a summary table and a table
   of contents, and say "read only the `ID` section you need".
7. **Structured data is authoritative for relations.** An N×M mapping is authoritative as
   JSON or CSV; Markdown is a generated view along the main access pattern. Reverse lookups,
   aggregation and integrity checks run against the structured data via a script. Only a
   "see everything at once" matrix may be authoritative as a Markdown table.
8. **Do not keep empty columns.** Drop a column that is empty for every row, or label it
   explicitly as not yet populated, so nobody assumes data exists.

## Safety: two layers [MUST]

Safety rules that can be judged deterministically are enforced by hooks; prose is only the
first line. Guard skill names are org-specific, so mekiki reads them from `config.json`
(`guards`) and skips any tier with no configured name — a hardcoded name would demand a
skill that does not exist in another organisation.

| Risk | Required |
|---|---|
| Billing | `requires: [<billing guard>]` **and** `disable-model-invocation: true` |
| Destructive write | `requires: [<write guard>]` or a write guard script |
| Browser automation | `requires: [<browser guard>]` |
| External publication | Human confirmation stated in Contract Preconditions **and** `disable-model-invocation: true` |

**A side-effecting skill must set `disable-model-invocation: true` [MUST].** Unlike
`requires`, this is the one activation control **the runtime actually enforces**: the model
can no longer choose the skill, and only an explicit human `/<skill>` runs it. The official
guidance uses deploy, commit and outbound-message skills as the examples — you do not want
a model deciding to deploy because the code looks ready. Use `disallowed-tools` when you
only need to remove a tool for the duration.

- A billing skill without the flag is **L20** (warn). External publication and destructive
  writes are equally required by this convention but are **not** mechanically checked:
  their detection wording hit 68% of a real corpus, which makes the finding unactionable.
- **Do not set it on a `flow.json` delegation target.** The flag also blocks programmatic
  invocation, so the orchestrator would no longer be able to call the skill. Put it on the
  orchestrator.
- It also removes the skill from the catalogue, so the always-on cost disappears too.

`requires` **does not start a guard by being declared** — the runtime never reads it. Write
an **imperative invocation step** in the body ("before doing any work, invoke
`billing-guard` with the `Skill` tool"). A passive reference gets bypassed; that is the
measured failure mode. A declared guard never mentioned in the body is **L18** (warn).

High-risk operations should be doubled up: a hook *and* an in-skill preflight, since
plugin-level hooks do not fire in every execution environment. Make writes idempotent and
route overwrites through a guard that requires an explicit `--force`.

## Evals [MUST]

**Use the official formats. Do not invent your own** — the same discipline this protocol
applies to master data. Schema details in [docs/DESIGN.md](docs/DESIGN.md); the primary
source is <https://agentskills.io/skill-creation/evaluating-skills>.

```json
{"skill_name": "<skill>", "evals": [
  {"id": 1, "prompt": "<a realistic user request>", "expected_output": "<what success looks like>",
   "files": ["evals/files/input.csv"], "assertions": ["a checkable statement"]}
]}
```

- **Write assertions after the first run.** You rarely know what "good" means until you have
  seen output. Make them checkable, observable, countable. Style and visual polish are not
  assertions — those go to human review.
- **Delete assertions that pass in both configurations.** They inflate the pass rate without
  demonstrating any value from the skill.

### Measuring output quality [MUST] — proving the skill beats its absence

**Run each case twice: with the skill and without it.** When improving an existing skill,
the baseline is a snapshot of the previous version.

- Record a `delta` across three axes: **pass rate, wall time and tokens.** Judge whether the
  improvement is worth the token cost — this is the only way to substantiate
  [§Context minimality](#context-minimality-must) empirically.
- **If the model handles the task well without the skill, the skill is adding nothing.**
  Consider deleting it.
- Start each run from a **clean context** (a subagent, or a separate session).
- If the same eval flips between runs, either the eval is flaky or **the instructions are
  ambiguous**. Add an example to remove the ambiguity.

### Measuring activation accuracy [MUST] — because L0 decides half the accuracy

Activation is measured **statistically**. A single case proves nothing: models are
non-deterministic, and the same request activates a skill on one run and not the next.

- About 20 queries: **8-10 that should activate, 8-10 that should not.** Make the negatives
  **near misses** — sharing vocabulary or concepts but belonging to a different skill.
  Unrelated queries test nothing.
- **Run each query three times** and compute a trigger rate (0.5 is a reasonable threshold).
- **Split train 60% / validation 40%.** Only train failures may guide edits; validation only
  checks that the edits generalise. Pick the best iteration by validation pass rate — the
  last iteration is often overfitted, not best.
- Do not paste a failing query's exact words into the description; that is overfitting.
  Write the **general category** those queries represent.

**L13** checks format only (error). **The absence of evals, and running them, is
unenforced** — do not build a runner; `skill-creator` automates this workflow.

### Recording the outcome [SHOULD]

Carrying the eval files proves the cases exist, not that they pass. Write what happened to
`evals/results.json`, which **L25** reads and the maturity tier below consults:

```json
{
  "ran_at": "2026-09-03",
  "evals": [{"id": "case-1", "pass": true}, {"id": "case-2", "pass": false}],
  "queries": {"total": 20, "correct": 18}
}
```

Both keys are optional, so recording one kind of run is fine. **An absent file says
nothing and is never a finding** — this is what lets an existing corpus adopt the practice
gradually. A file that exists must be well formed (error), and a failing case is a warning.
Trigger accuracy is recorded but not judged: a pass mark would need a threshold, and there
is no measurement that could set one honestly.

## Maturity tiers

A mekiki-specific scale — no maturity model exists in the official specification. The design
mirrors SLSA's assurance levels (cumulative 0-3, where L0 is simply "not yet"), and each
tier's conditions **map one-to-one onto lint rules**, so a tier is a measured position
rather than an aspiration. Note that the official documentation uses "tier 1-3" for the
*progressive disclosure* loading stages (catalogue → body → resources). That is a different
concept; do not conflate them.

| Tier | Condition (cumulative; judged as "no finding from the mapped rules") | Applies to |
|---|---|---|
| 0 — exists | No requirements (the same floor as SLSA L0) | — |
| 1 — loads, and has a contract | No L1/L2/L3 errors **and** all six Contract entries (L4) | **The minimum for every new skill** |
| 2 — deterministic and safe | No L6, L8, L9, L18 or L20 findings | **Required for high-risk skills** |
| 3 — verified | Both official eval formats present, no L13 error, and no L25 finding — that is, any recorded run passed | Core skills and anything under an orchestrator |

Semantics worth pinning down, because they are easy to misread:

- **Nothing applicable counts as satisfied.** A knowledge skill with no deterministic work
  and no risk wording vacuously satisfies tier 2 — having nothing to guard is a safe state.
- **A suppression with a reason counts.** Suppressions require a stated reason, so that
  judgement is part of the tier.
- **The tier is a headline; the per-axis truth is the Atlas badges** (contract dots, scr,
  test, eval). A single ladder inherently hides progress on one axis behind a gap on
  another — the reason SLSA split into tracks in v1.0 — so read per-axis progress from the
  badges.
- **Scope: this measures the engineering quality of your own skills.** Supply-chain trust
  for third-party skills — malware, prompt injection, author identity — is out of scope.
  Accepting an external skill is governed by "read it before you trust it" in
  [§Frontmatter](#frontmatter) and by your organisation's review process.

## Enforcement

The first principle of this document — do not expect prose to be followed — applied to the
document itself. This table says which conventions are mechanically enforced and which rest
on review. "New / pre-existing" refers to absence from / presence in the baseline file.
**Warnings and unenforced rules pass CI.**

| Convention | Rule | Severity |
|---|---|---|
| Official name constraints (64 chars, charset, directory match) [MUST] | L1 | new error / pre-existing warn |
| Naming shape (gerund, `knowledge-*`) [SHOULD] | — | **unenforced** (enforcing it mis-flagged 25 valid names while 104 slipped through a catch-all) |
| description 150-400 chars, trigger wording (combined with `when_to_use`), 1,536-char listing cap [MUST] | L2 | length: new error / pre-existing warn. Trigger and overflow: warn |
| No lifecycle wording in description [MUST] | L3 | error |
| Contract [MUST] | L4 | missing Non-goals: error. Others: new error / pre-existing warn |
| Directory conventions [MUST] | L5 / L10 | new error / pre-existing warn. Build artifacts: error |
| Determinism boundary [MUST] | L6 | **warn only** (heuristic) |
| Script interfaces [MUST] | — | **unenforced** (review) |
| Tests beside `scripts/` [MUST] | L9 | new error / pre-existing warn |
| Before writing a skill: the ladder [MUST] | — | **unenforced** (the first question in review; `mekiki new` reminds you) |
| Context minimality: 500 lines and 5,000 tokens [MUST] | L19 | **warn only** (size is a proxy) |
| Context minimality: tests 1-5 [MUST] | — | **unenforced** (review) |
| Single source of truth [MUST] | L7 / L14 | error (warn with `canonical`/`duplicate_of` or a generated banner) |
| `user-invocable` on internal skills [MUST] | L11 | error |
| Non-standard frontmatter keys | L12 | warn |
| Risk guard `requires` declaration [MUST] | L8 | error, but only for tiers with a guard configured in `config.json` |
| Imperative guard invocation in the body [MUST] | L18 | warn (only presence of the name is mechanically checkable) |
| `disable-model-invocation` on side-effecting skills [MUST] | L20 | **billing only, warn**. External publication and destructive writes: **unenforced** (wording hit 68% of a corpus) |
| `flow.json` schema, existence of targets, ordering [MUST] | L15 / L16 / L17 | error / error / **warn** |
| Every intermediate artifact is consumed [SHOULD] | L24 | warn (the last phase is exempt) |
| Distinct trigger phrases between skills [MUST] | L23 | warn on a quoted phrase two skills both claim |
| `requires` / `depends_on` name an existing skill [MUST] | L21 | warn (a partial lint cannot resolve a cross-plugin reference) |
| Links into `references/`, `scripts/`, `assets/`, `evals/` resolve [MUST] | L22 | warn. **Prose mentions of a path: unenforced** (measured as examples, not references) |
| Existence of `flow.json` [MUST] | — | **unenforced** (orchestrators cannot be identified mechanically) |
| Evals in an official format [MUST] | L13 | schema violation: error. Legacy `cases.json` or an unknown name: warn. **Absence: unenforced** |
| Eval methodology (with/without, activation statistics) [MUST] | — | **unenforced** (delegated to `skill-creator`) |
| Recorded eval outcome in `evals/results.json` [SHOULD] | L25 | malformed: error. A failing case: warn. **Absence: unenforced** |
| Artifact paths, numeric SSOT `_ref` [MUST] | — | **unenforced** (review) |
| Artifact IR, atomic deliver, simplification markers [SHOULD] | — | **unenforced** (review) |
| Master data format 1-8 [MUST] | — | **unenforced** (PK/FK checks belong to the package's own script) |

## Suppression

Suppress a false positive with a stated reason. **A suppression without a reason is
invalid** — the reason is what makes the exception reviewable.

```markdown
<!-- mekiki: disable L6 -- explains judgement criteria; the arithmetic lives in scripts/calc.py -->
```
