---
name: getting-started-with-mekiki
description: Takes someone from zero to their first mekiki run - installs the binary, finds their skill corpus, runs the first lint and atlas, and explains the result in plain terms. Use this when the user wants their skills looked at without naming a workflow ("check my skills", 「スキルの調子を見たい」), or asks what mekiki is or where to start. For the audit-and-repair workflow, auditing-skill-corpus takes over.
---

## Contract
- **Trigger**: the user wants their skills looked at but names no command or workflow, or asks what mekiki is, whether it is set up, or where to begin.
- **Inputs**: optional: a path to a skill tree. Without one, this skill finds the candidates itself and asks the user to pick — it never guesses which corpus was meant.
- **Preconditions**: none. A missing binary is not a blocker; installing it is step 1. Only a missing Go toolchain stops this skill, and then it says exactly what to install.
- **Outputs**: `skill-atlas.html` in the working directory, plus a plain-language first-look summary in the conversation.
- **Postconditions**: mekiki runs, the user knows their corpus size and error/warning counts, has the Atlas open, and has been told the one next step that fits their situation.
- **Non-goals**: does not repair findings or produce a repair plan, and does not set up CI gates or baselines beyond explaining them — auditing-skill-corpus owns the operating workflow; does not judge how a skill is written (a review skill's job); does not create skills (`mekiki new`, or skill-creator).

## Steps

### 1. Make sure the binary exists

```bash
mekiki version
```

If that fails, install it — this is the expected case, not an error:

```bash
go install github.com/toritori0318/mekiki@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

No Go toolchain? Say so plainly ("mekiki is a single Go binary; install Go from
https://go.dev/dl/ and rerun") and stop. Do not attempt substitute tooling.

### 2. Find the corpus — ask, never guess

If the user gave a path, use it. Otherwise look in the usual places and **present what was
found as a choice**:

```bash
for d in ~/.claude/skills ~/.claude/plugins ./skills ./plugins .; do
  [ -d "$d" ] && echo "$d: $(find "$d" -name SKILL.md 2>/dev/null | wc -l) skills"
done
```

Two corpora with a plausible claim (say, a personal `~/.claude/skills` and a project tree)
are the user's decision, not a coin toss. Multiple paths can be passed in one run.

### 3. First look — summary first, findings later

```bash
mekiki lint <path>
```

Read the **last line and the notes**, and translate rather than recite:

- *"N errors"* — things mekiki is sure about; they fail a CI gate.
- *"M warnings"* — things a human should look at; they never fail a build, on purpose,
  because those rules can be wrong.
- The *"no baseline"* note — one sentence: mekiki is treating every skill as pre-existing
  (gentler severities); a baseline is how new skills get held to a higher bar, and it can
  wait until the user cares about CI.

Do **not** paste the full findings list into the conversation on a first run. Offer the top
three errors, at most, with one line each on what they mean.

### 4. One page they can actually look at

```bash
mekiki atlas <path> --out skill-atlas.html
```

Tell the user to open it, with one line per view: **Board** — what you have, as named tiles;
**Flow** — your workflow skills as pipelines; **Catalog** — a sortable table, opened on
"most errors first". Everything clicks through to the same detail panel.

### 5. Hand off with one next step, not a menu

Match the situation and name a single move:

| The user's situation | The one next step |
|---|---|
| Inherited corpus, lots of findings | invoke auditing-skill-corpus — it starts with the baseline, gates on the delta, and turns findings into a repair plan |
| Wants a specific finding fixed | also auditing-skill-corpus; its playbook says how to repair each rule |
| Asks whether a skill is well written | a skill-review skill, not mekiki — mekiki checks structure, not prose |
| Wants to write a new skill | `mekiki new <name> --out <dir>` — the skeleton is compliant from birth |

## Gotchas

- **Exit code 1 is not a crash.** It means "errors exist" and is exactly what CI keys on. A
  first run on a real corpus exiting 1 is normal, not something to apologise for or retry.
- **mekiki never writes into the tree it inspects**, so running it on anything — including a
  corpus the user does not own — is safe. The only files it creates are the ones asked for
  (`skill-atlas.html`, and `baseline.json` only via `--update-baseline`).
- **The Atlas embeds every description in the corpus.** Fine to open locally; not something
  to commit or post publicly without the same care as the corpus itself.
- **Do not run `--update-baseline` as part of a first look.** It freezes today's inventory
  as "pre-existing", which is an operating decision the user should make knowingly — that
  conversation belongs to auditing-skill-corpus.
