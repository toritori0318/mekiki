# mekiki design specification

**Audience:** anyone implementing or extending mekiki, and anyone writing a new skill who
needs the exact rule definitions.
**Relationship to other documents:** [SKILL_PROTOCOL.md](../SKILL_PROTOCOL.md) states the
conventions for authors; this document states how they are checked.
**On the measured figures:** numbers and example shapes come from the corpus mekiki was
first built against (6 plugins, ~190 skills). Read them as evidence for the design, not as
constraints on your environment.

日本語版: **[DESIGN.ja.md](DESIGN.ja.md)**

---

## 1. Purpose

Once a skill corpus grows past a few dozen, the same five defects recur regardless of
author: deterministic work left in prose, shared files physically copied, activation left
unspecified, guard declarations that start nothing, and no verification at all. This
document defines the mechanical checks that prevent them.

## 2. Scope

**In scope:** the frontmatter schema, naming, the Contract block, directory layout, data
formats, guards, evals, the rule definitions, the CLI surface and the test cases.

**Out of scope:** fixing an adopting organisation's existing skills; the guard scripts
themselves (each organisation implements its own); running evals (schema only — execution is
delegated to `skill-creator`, see §4.8).

## 3. Constraints

- **Go 1.24+, standard library only.** No third-party dependencies: the binary should have
  no supply chain of its own, which matters for a tool whose subject is supply-chain
  discipline. This is why the frontmatter parser, flag handling and HTML embedding are all
  hand-rolled or `embed`.
- **Any skill repository is a valid target.** Plugins may sit under `plugins/<plugin>/` with
  skills at `plugins/<plugin>/skills/<skill>/SKILL.md`, or skills may sit directly under a
  `skills/` directory. The plugin name is the parent of `skills/` when present, otherwise
  the grandparent of `SKILL.md`.
- **Frontmatter is YAML, parsed by a hand-written subset parser.** It handles flat
  `key: value`, one level of nesting, inline lists, `- ` item lists, and **block scalars
  (`|` and `>`), joining the following indented lines into one value** — required because
  real corpora contain skills whose description is a block scalar. Parsing is lenient:
  uninterpretable lines become warnings rather than aborting the file (the official
  client-implementation guide recommends the same).
- **The inspected tree is read-only.** mekiki never writes to it; generated files
  (baseline, atlas output) are written where the user asks.

## 4. Data model

### 4.1 Frontmatter schema

Keys fall into three groups: the **Agent Skills specification**
(<https://agentskills.io/specification>), the **Claude Code extensions**, and
**protocol-specific** keys. The runtime interprets the first two; only mekiki reads the
third. Official keys must never be reported as non-standard (they are in L12's allow-list).

#### Required (absence is an error)

| Key | Type | Official constraint | This protocol adds |
|---|---|---|---|
| `name` | string | 1-64 chars; lowercase alphanumerics and hyphens; no leading, trailing or consecutive hyphens; equals the parent directory name | — |
| `description` | string | 1-1024 chars; states what it does and when to use it | 150-400 characters, and the content requirements in §4.3 |

#### Optional, Agent Skills specification

| Key | Type | Notes |
|---|---|---|
| `license` | string | License name, or the name of a bundled license file |
| `compatibility` | string | Max 500 chars. Required products, packages, network access. Most skills need none |
| `metadata` | map | Arbitrary string→string map |
| `allowed-tools` | string/list | Tools pre-approved for the invoking turn (experimental). **A grant, not a restriction** — a skill can hand itself broad access, so read third-party skills before trusting them |

#### Optional, Claude Code extensions

| Key | Type | Notes |
|---|---|---|
| `when_to_use` | string | Extra activation context. **Concatenated onto the description; the listing shows the combination up to 1,536 characters** and truncates the rest, which then never reaches the activation decision (checked by L2). Other clients do not read it, so portable skills use the description |
| `paths` | string/list | Auto-activate only for matching files. A mechanical way to prevent over-firing (§4.3) |
| `disable-model-invocation` | bool | `true` **stops the model activating the skill**; only an explicit human `/<skill>` runs it. Required for side-effecting skills (§4.6) |
| `disallowed-tools` | string/list | Tools removed from the pool while the skill is active |
| `user-invocable` | bool | `false` **only hides it from the `/` menu**; invocation through the Skill tool still works. Different from `disable-model-invocation` |
| `context` | string | `fork` runs the skill in a subagent (context isolation) |
| `agent` | string | Subagent type when `context: fork` |
| `background` | bool | `false` awaits the forked result in the invoking turn |
| `model` / `effort` | string | Overrides while the skill is active. Useful for pinning work that needs determinism |
| `hooks` | map | **Hooks scoped to this skill's lifetime** (all events, cleaned up on exit) |
| `arguments` | string/list | Named positional arguments; `$name` and `$ARGUMENTS` expand in the body |
| `argument-hint` | string | Hint shown during `/` completion |
| `shell` | string | Shell for inline commands in the body (`bash` or `powershell`) |

#### Optional, protocol-specific (the runtime ignores these)

| Key | Type | Becomes required when |
|---|---|---|
| `status` | `active` \| `deprecated` \| `experimental` | The skill is retired or experimental. Defaults to active |
| `canonical` | `<plugin>:<skill>` | A same-named or same-content skill exists elsewhere; required on every copy but the source |
| `duplicate_of` | `<plugin>:<skill>` | Placed on an intentional copy (an L7 downgrade path) |
| `requires` | list[string] | Risk wording appears in the body (§4.6) |
| `depends_on` | list[string] | A prior skill's artifact is an input (§4.5) |

`requires`, `depends_on`, `status`, `canonical` and `duplicate_of` are **not interpreted by
the runtime**. In particular, declaring `requires` does not load a guard — L18 checks for
the imperative invocation in the body that actually does. When you want the runtime to
enforce something, `disable-model-invocation: true` is the mechanism that works.

The values of official keys (the syntax of `allowed-tools`, the content of `compatibility`)
are not validated. Note that the official reference implementation `skills-ref` states it is
"intended for demonstration purposes only" and "not meant to be used in production", so
**CI validation must not be delegated to it**; anything worth checking is implemented here.

#### Prohibited (error)

- Lifecycle wording inside `description`: `deprecated`, `obsolete`, `superseded`, and their
  Japanese equivalents. Move it to `status:`.

### 4.2 Naming

Only the official constraints are checked (L1):

| Check | Rule |
|---|---|
| Charset and shape | `^[a-z0-9]+(-[a-z0-9]+)*$` — lowercase alphanumerics and hyphens, no leading, trailing or consecutive hyphens |
| Length | 1-64 characters |
| Match | frontmatter `name` equals the parent directory name |

Severity is **error for new skills, warn for pre-existing ones** (§8; "pre-existing" means
present in the baseline file).

Category shapes (gerund for actions, `knowledge-<domain>`, a noun of up to three words for
utilities) are a **SHOULD and deliberately unchecked**. Enforcing the three regexps was
tried and collapsed under measurement:

- All 25 findings were **valid official names** (`cloud-ads-account-setup` and similar
  `<domain>-<object>-<action>` shapes, 15% of the corpus). Renaming breaks existing
  references, so the finding was not actionable.
- Meanwhile 104 skills (62%) passed through the "noun, three words" catch-all
  (`^[a-z0-9]+(-[a-z0-9]+){0,2}$`), so action skills like `app-setup` bypassed the gerund
  requirement entirely — **the requirement never bound**.
- The problem actually worth catching, a `name`/directory mismatch, occurred **zero times**,
  and the old implementation had no test for that branch at all.
- A name is not the primary driver of activation — the official guidance is explicit that the
  description is "the primary mechanism agents use to decide whether to load a skill".

### 4.3 Description content

Three elements, in this order (L2 checks what is mechanically checkable):

1. **What it does** — third person, functional.
2. **When it activates** — concrete symptom and request wording (quoted request forms work
   well).
3. **When it does not activate** — redirect to the neighbouring skill.

Target 150-400 characters. Trigger wording is judged on `description` + `when_to_use`
combined, and a combination over 1,536 characters is reported because the listing truncates
it.

`paths` is the mechanical counterpart: when a skill only applies to certain file types, a
glob restricts activation more reliably than any wording can.

### 4.4 The Contract block

The first `##` heading in the body is `## Contract`, containing six entries in
`- **Name**:` form:

```markdown
## Contract
- **Trigger**: <one sentence; must not contradict the description>
- **Inputs**: <required / optional, and how each is obtained>
- **Preconditions**: <what must hold before running; a verification command if possible>
- **Outputs**: <generated paths (§4.5) and formats>
- **Postconditions**: <completion criteria; the command for a scripts gate if one exists>
- **Non-goals**: <what this does not do, and which skill or stage owns it>
```

Severity is two-tiered: **a pre-existing skill with no Contract at all is a warn** (so
adoption does not flood a corpus with errors), while **a Contract that exists but omits
`Non-goals` is an error**. Other missing entries follow new-error / pre-existing-warn.

Prefer *executable gates* over sentences for Pre/Postconditions: a read-only script that
exits non-zero until the work is genuinely complete.

### 4.5 Artifact paths

```
<output_base>/<target>/<skill-name>/{YYYYMMDD}_<slug>/<NN>_<artifact>.<ext>
```

- Directory names are **ASCII alphanumerics and hyphens only**.
- Artifacts under an orchestrator carry an `NN_` numeric prefix to express order.
- Numbers have a single source of truth in one file; downstream artifacts reference it
  (`_ref`). Recomputation and hand-copying are prohibited.

### 4.6 Risk tiers and required guards

When the body contains the detection wording below, `requires` must declare the
corresponding guard. **Guard names are org-specific and read from `config.json` `guards`**
(§6, L8); a tier with no configured name is not checked. The names in the table are an
example configuration.

| Risk tier | Detection | Required guard | `disable-model-invocation` |
|---|---|---|---|
| Billing | ad spend, billing, invoice, budget-set … / 広告費・課金・請求・予算設定 | `guards.billing` | **Required. Checked by L20 (warn)** |
| Destructive write | mutation, delete, uninstall, overwrite, DROP … / 上書き・アンインストール | `guards.write`, or a write guard script | Required (unenforced) |
| Browser automation | browser, playwright, puppeteer … / ブラウザ操作 | `guards.browser` | — |
| External publication | publish, send email, broadcast … / 公開・配信 | Human confirmation in Contract Preconditions (org-independent, so always checked) | Required (unenforced) |

**`disable-model-invocation: true` is, unlike `requires`, the one activation control the
runtime actually enforces.** Measured: only 4 of 190 skills had it. Mechanical checking is
scoped to billing — the detection wording for the other tiers hit 114 of 190 skills (68%),
which is not actionable. Because the flag also blocks programmatic invocation, **it must not
be set on a `flow.json` delegation target**, or the orchestrator can no longer call it.

Guards live in the plugin's `hooks/hooks.json` and/or a skill's own `hooks:` frontmatter
(all events, active only while the skill runs, cleaned up on exit). Skill-specific guards
belong in the frontmatter so they travel with the skill. Note that a project skill's
frontmatter hooks only run after the workspace trust dialog has been accepted.

High-risk operations should be doubled up with a hook *and* an in-skill preflight, since
plugin-level hooks do not fire in every execution environment.

### 4.7 Data format selection

| Use | Format |
|---|---|
| Prose, free-form | Markdown |
| Structured, diff-sensitive state | YAML |
| High-volume append-only history | JSONL (append-only) |
| Executable queries | `.sql` |
| Large relational joins and aggregation | SQLite plus a script |

- **Do not model relations as a Markdown table for the model to join.** Express relations by
  ID.
- Separate an immutable snapshot (baseline) from a mutable view (current).
- YAML is not for table data (anchors are not foreign keys, the parser is complex, and it
  scales badly). Its place is small state and configuration files.

### 4.8 Eval schema (the official Agent Skills formats)

**Do not invent a proprietary format** — the same discipline this protocol applies to master
data. Primary sources: <https://agentskills.io/skill-creation/evaluating-skills> and
<https://agentskills.io/skill-creation/optimizing-descriptions>.

Output quality — `<skill>/evals/evals.json`:

```json
{
  "skill_name": "<skill-name>",
  "notes": "<optional. Additional keys are permitted>",
  "evals": [
    {
      "id": 1,
      "prompt": "<a realistic user request>",
      "expected_output": "<a human-readable description of success>",
      "files": ["evals/files/input.csv"],
      "assertions": ["a checkable, observable, countable statement"]
    }
  ]
}
```

Activation accuracy — `<skill>/evals/eval_queries.json`:

```json
[
  {"query": "<a request that should activate the skill>", "should_trigger": true},
  {"query": "<a near miss that belongs to another skill>", "should_trigger": false}
]
```

- Required: top-level `skill_name` (string) and `evals` (list); each entry needs `id`,
  `prompt` and `expected_output`. Optional: `files` and `assertions` (lists). Additional
  keys such as `notes` are permitted.
- **Write assertions after the first run** — you rarely know what "good" means until you have
  seen output. Delete assertions that pass in both configurations; they inflate the pass rate
  without showing any value.
- Files produced during a run (`grading.json`, `timing.json`, `benchmark.json`,
  `feedback.json`) are not hand-written. The workspace layout is
  `<skill>-workspace/iteration-N/<eval>/{with_skill,without_skill}/`.
- **Do not build a runner.** `skill-creator` (github.com/anthropics/skills) automates the
  loop; L13 validates format only.
- The legacy proprietary `cases.json` is a migration target (L13 warn). It once defined its
  own `kind`/`expect`/`severity` schema, which collided with the official standard and caused
  the linter to penalise officially compliant files.

#### Methodology (the convention lives in SKILL_PROTOCOL.md)

Not enforceable, but this is where accuracy actually comes from:

| Target | Method | Judgement |
|---|---|---|
| Output quality | Run every case twice: with and without the skill | The `delta` in pass rate, time and tokens. If the model handles it well without the skill, the skill is unnecessary |
| Activation accuracy | ~20 queries (8-10 should-trigger plus 8-10 near misses), three runs each | `trigger_rate` against a 0.5 threshold. Split train 60% / validation 40% to avoid overfitting, and pick the best iteration by validation pass rate |

### 4.9 Flow declaration (orchestration)

An orchestrator that calls other skills in sequence carries `<orchestrator>/flow.json`:

```json
{"flow": [
  {"phase": 1, "skill": "<skill-name>", "inputs": ["<external input or artifact>"], "outputs": ["NN_artifact.ext"]}
]}
```

- `phase`: integer from 1, unique and ascending. `skill`: the delegate's directory name.
- `inputs` / `outputs`: artifact filenames, or identifiers for external inputs.
- `mekiki atlas` draws the flow exactly when this file exists. Without it, the page falls
  back to *inferring* a flow from a `### Phase Registry` table in the body and labels the
  result as inferred in the UI.
- Inference resolves fully qualified `plugin:skill` references by collecting every candidate
  token in the delegate cell and choosing one that names a real skill. Taking the first token
  would resolve to the plugin name and silently drop every row — which is exactly how one
  real orchestrator went undetected.

### 4.10 Context minimality (size discipline)

The convention is in SKILL_PROTOCOL.md; only the size proxy is mechanically checkable. The
official guidance has two halves — "under 500 lines and 5,000 tokens" (the specification's
progressive disclosure section recommends "Instructions (< 5000 tokens)"):

- The **body** (frontmatter excluded) is at most **500 lines**, and
- its **estimated token count** is at most **5,000**. Estimation is
  `skill.EstimateTokens`: **1.2 tokens per CJK rune, 1 per 4 other runes**, so no external
  tokenizer is needed (§3).
- **Line count alone fails on a CJK corpus.** Measured across 190 skills: bodies averaged
  ~76 characters per line, only one skill exceeded 500 lines, but 93 exceeded 5,000
  characters of body. Adding the token check moved detection from 1 finding to 44.
- The finding names where to move content: on-demand knowledge to `references/`,
  deterministic work to `scripts/`.

The always-on catalogue cost — `name` + `description` (+ `when_to_use`) loaded for every
skill every session — is measured but not gated: ~42,250 tokens across 174 skills, averaging
243, against an official expectation of 50-100. The Atlas surfaces it as a gauge.

## 5. Repository layout

```
mekiki/
├── main.go                      # subcommand dispatch, flag parsing, output
├── config.example.json          # template for org-specific settings
├── SKILL_PROTOCOL.md            # the conventions (authors read this)
├── docs/                        # usage, this document, the decision record, images
├── scripts/                     # maintenance tooling: demo corpus, README screenshots
├── skills/                      # the Agent Skill mekiki ships (§11)
└── internal/
    ├── skill/                   # discovery, frontmatter parsing, Contract, token estimate
    ├── config/                  # guards and detection patterns
    ├── lint/                    # rules L1-L25, orchestration, baseline, snapshots, diff, SARIF
    ├── scaffold/                # skeleton generation
    └── atlas/                   # page model plus the embedded template
```

Generated per environment and never committed: `baseline.json`, `config.json`, the atlas
output.

## 6. Rules

### CLI

```
mekiki lint  [PATH...] [--format text|json|sarif] [--severity error|warn]
                       [--baseline baseline.json] [--config config.json] [--update-baseline]
mekiki new   NAME      [--out DIR] [--type action|knowledge|util]
                       [--risk billing|write|browser|publish] [--config config.json]
mekiki atlas [PATH...] [--out skill-atlas.html] [--config …] [--baseline …]
                       [--base base.json]
mekiki diff  BASE.json HEAD.json [--format text|json|sarif]
```

- With no PATH, `MEKIKI_TARGET` is consulted; if that is unset it is an error. A PATH may be
  a plugin tree or a single skill directory.
- `--severity` filters **display only** and does not affect the exit code.
- `--format sarif` emits a SARIF 2.1.0 log for GitHub code scanning. On `diff` it carries
  the **added** findings only, so an inherited backlog is never annotated on a pull
  request. Result URIs are relative to the working directory when the file lies under it.
- Exit codes: **0** no errors, **1** errors present (even when filtered from display),
  **2** usage or runtime failure.
- `--config` defaults to `config.json`. **Absent is fine** — checks that depend on
  org-specific names are then skipped. Two keys:
  - `profile` — the runtime the corpus targets: `claude-code` (default) or `generic`.
    Selects the numeric limits and L12's official-key set, since the listing cap and the
    extension keys are Claude Code behaviours rather than specification rules. An unknown
    name produces a note and falls back to the default.
  - `limits` — `listing_cap`, `max_body_lines`, `max_body_tokens`. An entry overrides the
    profile's value; 0 disables that check.
  - `guards` — risk tier → guard skill names (L8, L20).
  - `patterns` — detection regexps for L2, L3, L6, L8 and L11. The built-in defaults are
    bilingual; an override replaces a key entirely and is case-insensitive unless it carries
    its own flag group. An invalid regexp produces a note and falls back for that key only.
- `--baseline` defaults to `baseline.json`; absent means every skill counts as pre-existing.
- `atlas --base` takes a `lint --format json` snapshot and marks the delta on the page:
  new and removed skills, added errors per skill, and resolved findings. Without it the
  page carries no delta data at all. Tiers are **not** compared — a snapshot does not record
  them, and recomputing one for the base tree would mean linting a corpus the page was not
  given. A snapshot from a mekiki that predates the `skills` list still diffs findings, and
  the page says why new and removed skills are missing rather than guessing.
- `mekiki diff` compares two `--format json` snapshots and reports only what a change
  **added** and **resolved**. Because warnings pass the gate, surfacing added warnings is
  what makes them reviewable in a pull request. The diff key is the **(rule, skill)
  multiset**: line and message are excluded because they drift between runs (a message
  embedding a count reads "637 lines" then "641 lines"), and keying on them would report one
  unchanged problem as an add plus a resolve. Exit 1 **only when an added finding is an
  error**.

### Rule list

Severity "new / pre-existing" refers to absence from / presence in the baseline.

| ID | Check | Detection | Severity |
|---|---|---|---|
| **L1** | `name` matches the official constraints and the directory name | §4.2 | new error / pre-existing warn |
| **L2** | description length, trigger wording, listing cap | Length on description alone; trigger wording on **description + when_to_use**; overflow past 1,536 combined characters | length: new error / pre-existing warn. trigger and overflow: warn |
| **L3** | No lifecycle wording in description | `patterns.lifecycle` | error |
| **L4** | `## Contract` is the first `##` heading and holds six entries | Heading parse plus `- **X**:` presence | No Contract (pre-existing): warn. Contract present but missing Non-goals: error. Others: new error / pre-existing warn |
| **L5** | References in `references/`, code in `scripts/` | Presence of `reference/`, `bin/`, `assets/scripts/`, `references/scripts/` | new error / pre-existing warn |
| **L6** | Deterministic wording in the body with no `scripts/` | `patterns.deterministic`, after stripping inline code, link targets, quoted strings and file paths | warn (heuristic; suppressible) |
| **L7** | Byte-identical files across skills | md5 of everything under `references/` across all skills; files under 8KB excluded | error. Downgraded to warn by a `canonical`/`duplicate_of` declaration, or by a generated banner (an `AUTO-GENERATED`-style marker **and** a regeneration command in the leading comment) |
| **L8** | Risk wording with no guard declaration | §4.6 detection × `requires`. **Guard names come from `config.json`**; unconfigured tiers are skipped. The publication check is org-independent and always runs | error |
| **L9** | `scripts/` has executable code but no test | Code (`.py .sh .js .ts .go .rb`) present while `tests/`, `test/`, `test_*`, `*_test.*` are all absent | new error / pre-existing warn |
| **L10** | Build artifacts inside the inspected tree | Working-tree walk for `*.zip`, `*.tar.gz`, `*.tgz`, **regardless of git tracking** (an untracked distribution zip is invisible to `git ls-files`) | error |
| **L11** | Internal skills lack `user-invocable: false` | `patterns.internal` in the description without the flag | error |
| **L12** | Non-standard frontmatter keys | Keys outside §4.1. Official keys (including `when_to_use`, `hooks`, `paths`, `model`) are exempt | warn |
| **L13** | Evals are in an official format | `evals/evals.json` and `evals/eval_queries.json` are validated against §4.8. Legacy `cases.json` prompts migration; any other filename is non-standard | error (schema) / warn (legacy or unknown name) |
| **L14** | The same skill name in multiple plugins | Name collision with no `canonical` declaration on any copy | error |
| **L15** | `flow.json` schema | Present-only. Each element needs `phase` (int), `skill` (str), `inputs` and `outputs` (lists); phases unique and ascending | error |
| **L16** | `flow[].skill` names an existing skill | Cross-checked against every discovered skill name | error |
| **L17** | Flow ordering | An input that another phase produces at the same or a later phase | warn |
| **L18** | A guard in `requires` never appears in the body | Substring presence of each `requires` entry in the body. The declaration is not interpreted by the runtime, so this backs the imperative invocation step | warn (only presence is mechanically checkable) |
| **L19** | Body size discipline | §4.10: over 500 lines, or over 5,000 estimated tokens | warn (size is a proxy; suppressible) |
| **L20** | Side-effecting skills lack `disable-model-invocation` | Billing wording × the flag. **Exempt:** `flow.json` delegation targets (the flag would stop the orchestrator calling them) and configured guard skills (they describe the risk without performing it). Other risk tiers are out of scope — their wording hit 68% of a corpus | warn (heuristic; suppressible) |
| **L21** | `requires` / `depends_on` names a skill that does not exist | Cross-checked against every discovered skill name and `plugin:name` key. Configured guard names count as existing, because L8 demands that same declaration and the two rules must not contradict each other | warn (a lint pointed at one plugin cannot resolve a cross-plugin reference) |
| **L25** | Recorded eval results | `evals/results.json`: `evals[{id, pass}]` and `queries{total, correct}`, both optional but at least one required. Absence is silent. Trigger accuracy is recorded, not judged | malformed: error. A failing case: warn. Either blocks tier 3 |
| **L24** | A phase produces an artifact no later phase consumes | `flow.json` outputs cross-checked against later phases' inputs. The last phase is exempt: its outputs are the deliverable | warn (an earlier phase may legitimately end a branch) |
| **L23** | Two skills claim the same quoted trigger phrase | Quoted phrases (`「」`, `『』`, `"` and `“”`) in description + `when_to_use`, from four runes up, excluding single words in a space-separated language and anything containing `${`. Silent when a group member declares `canonical`/`duplicate_of` | warn (activation is the model's, so this is evidence rather than proof) |
| **L22** | A markdown link into the skill's own directories does not resolve | Link targets under `references/`, `scripts/`, `assets/` or `evals/`, anchors stripped, directories accepted. Prose mentions are **not** checked — measurement showed they are examples or another skill's files, never a claim about a local file | warn (a `references/` file may be generated rather than committed) |

### Detection patterns

Word boundaries are applied asymmetrically, because the two families of rule fail in
opposite directions:

- **Safety rules (`risk_*`)** produce error-severity findings, so a miss is worse than a
  false positive. Their terms are distinctive enough to stand without boundaries — and
  boundaries actively hurt, because Go's RE2 counts `_` as a word character, so
  `\bbrowser\b` fails to match `09_browser-operation-notes.md`.
- **Quality heuristics (`deterministic`)** produce warnings a human triages, so noise is the
  greater cost. These are bounded on both sides and stemmed narrowly: an unbounded "ratio"
  also matches generation, iteration and migration (61 false positives on a real corpus),
  and "comput" matches "computer.type", a tool name rather than a calculation.

The prose rules also strip inline code, link targets, quoted strings and file paths before
matching. A skill name in backticks or a filename is a reference, not a description of work;
this accounted for every remaining L6 false positive.

### Suppression

```markdown
<!-- mekiki: disable L6 -- explains judgement criteria; the arithmetic lives in scripts/calc.py -->
```

A suppression with no stated reason is invalid. Suppressions written for the previous tool
name (`skill-lint: disable`) continue to work.

## 7. Processing

### lint

1. Resolve PATHs and enumerate directories containing `SKILL.md` (symlinked targets once
   only).
2. Load the baseline and the config; mark each skill new or pre-existing.
3. Per skill: with unparseable frontmatter, emit one `L0` error and **skip the other rules**
   (they cannot say anything useful). Otherwise emit `L0` warnings for uninterpretable lines
   and run the single-skill rules.
4. Run the cross-cutting rules (L7, L10, L14, L16, L20, L21, L23).
5. Apply suppressions, de-duplicate, and sort deterministically by severity → skill → rule →
   line, so CI output can be diffed.

### new

1. Validate the name against the official constraints (§4.2) and reject reserved words
   (`anthropic`, `claude`) **before writing anything**. Refuse to overwrite an existing
   directory.
2. Generate `SKILL.md` (frontmatter, `## Contract` with all six entries, `## Steps`,
   `## Gotchas`, with `[TODO]` markers), plus both official eval templates and
   `references/.gitkeep`.
3. `--type knowledge` sets `user-invocable: false` (L11).
4. With `--risk`: read the guard name from `config.json` and write both `requires` and **the
   imperative invocation step in the body** (L18 — the declaration alone starts nothing). If
   no guard is configured, leave a `[TODO]` comment and **never invent a name**. `billing`
   and `publish` also get `disable-model-invocation: true` (L20).
5. Print the remaining `[TODO]` count and a reminder of the "a new skill is a last resort"
   ladder.

A test asserts that the generated skeleton produces no findings other than the expected L2
(the description is still a placeholder) — the skeleton must not violate the conventions it
teaches.

## 8. Edge cases

| Case | Behaviour |
|---|---|
| No frontmatter, or `---` never closes | One `L0` error; the remaining rules are skipped for that skill |
| Frontmatter the subset parser cannot fully read | Evaluate on the keys that parsed and add an `L0` warning per unreadable line |
| Block scalar (`description: \|` or `>`) | Following indented lines are joined into one value and evaluated as such, so L2 and L3 do not misfire |
| Baseline absent (first run) | **Every skill counts as pre-existing** (severity errs toward warn) plus a note suggesting `--update-baseline` |
| Config absent | Checks depending on org-specific names are skipped; everything else runs |
| Symlinked skill | Evaluated once, by resolved path |
| Duplicate references under 8KB (L7) | Excluded, to avoid coincidental matches between template fragments |
| L6 false positive | Suppress with a stated reason |
| Duplicate findings | De-duplicated on the whole finding |
| A plugin with no skills | Skipped silently, not an error |

## 9. Acceptance

### 9.1 Regression tests

Fixtures in `internal/lint` are **reduced copies of defects observed in a real corpus**,
with names neutralised:

| Rule | Fixture | Expected |
|---|---|---|
| L2 | `enriching-product-page` (38-character description, no trigger wording) | error + warn |
| L3 | `legacy-sheet-setup` (description opens with lifecycle wording) | error |
| L5 | `querying-warehouse` (singular `reference/`) | warn (pre-existing) |
| L6 | `flash-sale` (division and a 1.3x threshold in prose, no `scripts/`) | warn |
| L7 | Shared knowledge over 8KB, md5-identical across two plugins | error |
| L8 | `paid-ads-setup` (billing wording, no guard declared) | error |
| L10 | `legacy-bundle.zip` (an **untracked** distribution zip — the regression that `git ls-files` cannot catch) | error |
| L11 | `resolving-queue-tasks` (declares itself internal without the flag) | error |
| L14 | `knowledge-shared-master` (same name in two plugins, no canonical) | error |
| L1 | Synthetic (`MySkill_v2`, `pdf--processing`, a 65-character name, a name/directory mismatch) | new error |
| L9 | `signup-form-setup` (`scripts/` present, no test) | warn (pre-existing) |
| L12 | `flash-sale` (`x-note`) plus a skill carrying every Claude Code key | warn / no finding |
| L13 | Both official formats, a schema violation, legacy `cases.json`, an unknown filename | no finding / error / warn / warn |
| L19 | Over 500 lines, and separately over the token budget while under 500 lines | warn each |
| L20 | Billing without the flag, with the flag, a guard skill, a flow delegation target | warn / none / none / none |
| L21 | An existing target, the `plugin:name` form, a missing target, a configured guard name | none / none / warn / none |
| L22 | A broken link, an existing file, a directory link, an anchor, a repeated link, prose mentions | warn / none / none / none / deduped / none |
| L23 | A phrase shared by two skills, a short phrase, a single English word, a `${VAR}` phrase, a declared duplicate, a vendored copy | warn each / none / none / none / none / one finding |
| L24 | An intermediate output nobody reads, a fully consumed flow, the last phase's output, an unparseable flow | warn / none / none / none |
| L25 | No results file, an all-pass run, a failing case, a queries-only run, malformed JSON, a report measuring nothing | none / none / warn / none / error / error |
| Parser | `building-context-from-handover` (block scalar description) | no false positives in L2 or L3 |

### 9.2 Definition of done

1. Every case above passes.
2. A corpus of a few hundred skills lints within 60 seconds, with exit code and JSON output
   as specified.
3. A skill from a corpus that followed the conventions voluntarily has the fewest errors of
   any plugin — the conventions must rate good design highly.
4. A skeleton from `mekiki new` produces zero errors once the `[TODO]`s are filled in.
5. Suppression works, and a suppression without a reason does not.
6. The bundled skill (§11) lints clean. A rule that the one skill written to satisfy it
   cannot satisfy is a rule to reconsider, and a test asserts this on every run.

### 9.3 Method

Test-first. The table in §9.1 is the initial test list: translate one case at a time into a
failing test, implement, refactor, and add to the list whenever a gap or a false positive
turns up while implementing.

**Then measure the rule against a real corpus.** Every significant bug in this tool's
history — unbounded English stems, a word-boundary regression in a safety rule, identifiers
counted as prose — was invisible to unit tests and obvious in a corpus run. The differential
mode (`mekiki diff` over two snapshots) is the cheapest way to see what a change actually
did.

## 10. Non-functional requirements

- Performance: a full scan of ~200 skills and a few thousand reference files within 60
  seconds. md5 work is bounded by excluding files under 8KB.
- Dependencies: Go standard library only. No network access. Read-only, except for files the
  user explicitly asks to be written.
- Output stability: findings are ordered deterministically (severity → skill → rule → line),
  so CI output and snapshots can be diffed.

## 11. The bundled skills

`skills/` ships two Agent Skills that drive this tool, split by situation rather than by
feature. `auditing-skill-corpus` exists because the findings mekiki prints are
self-explanatory while the **order the work happens in** is not, and that ordering is where
an agent goes wrong: it lints an inherited corpus and starts editing, when the first move is
to settle the baseline; it reads warnings as a to-do list; it repairs a prose-heuristic
finding by deleting the sentence; and, asked to satisfy a guard rule with no guard
configured, it invents a name. `getting-started-with-mekiki` covers the contact *before*
any of that is relevant — the binary is not installed, the user does not know which corpus
they mean or what an error count implies — and deliberately stops where operating decisions
begin: it never takes the baseline decision and never repairs.

| Path | Holds |
|---|---|
| `auditing-skill-corpus/SKILL.md` | The ordering: baseline first, the command per question, how to sort the output, what to ask rather than decide |
| `auditing-skill-corpus/references/rule-playbook.md` | Per-rule repair guidance, grouped by the repair rather than by rule number |
| `getting-started-with-mekiki/SKILL.md` | The first contact: install, discover the corpus (ask between candidates), first lint and Atlas, plain-terms summary, one next step |
| `*/evals/` | Both official eval formats per skill; each one's near-misses aim at the other and at the neighbouring skills |

The two must not compete for activation, so they quote **disjoint trigger phrases** (L23
checks this on every lint of `skills/`) and each names the other in its description and
Non-goals: first-ever run → getting-started; anything after that → auditing.

Two structural notes. The repair guidance sits in `references/` both because it is needed
only once a finding is in hand (§Context minimality's layer test) and for a mechanical
reason: it has to quote the risk vocabulary that L8 and L20 detect, and those rules read the
description and body only — in the body it would trip the very rules it explains. And the
skill claims the *mechanical* audit only; judgement about how a skill is written belongs to a
review skill, which its Non-goals names explicitly so the two do not compete for activation.

**The invariant:** `mekiki lint skills` reports nothing, and `TestBundledSkillPassesItsOwnLinter`
asserts it. A new rule that the skills written to satisfy the conventions cannot satisfy
is a rule to reconsider before it ships.
