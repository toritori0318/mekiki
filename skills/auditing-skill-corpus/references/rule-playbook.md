# Repair playbook

What to *do* about a finding. The rules themselves are defined in mekiki's `docs/DESIGN.md`
and the conventions behind them in `SKILL_PROTOCOL.md`; neither says how to repair, which is
what this file is for. Rules are grouped by the repair they want, because the group decides
the move far more often than the individual rule does.

The organising idea: **a finding names the layer that is wrong, and the repair belongs in
that layer.** Deleting the sentence that tripped a rule removes the evidence and leaves the
defect in place. It is the wrong move almost every time.

---

## The description is doing the wrong job — L2, L3, L11, L23

The description is the whole activation mechanism, so these are not cosmetic.

- **L2, too short or too long.** Under 150 characters it cannot carry the three things a
  description needs; over 400 it dilutes the words that decide activation. Rewrite to: what it
  does (third person) → when it fires, in the user's own wording → what it does *not* cover,
  naming the neighbouring skill.
- **L2, no trigger wording.** Add the situations in the phrasing a user actually types. Not
  "for report generation" but "use this when the user asks for the weekly report".
- **L2, past the listing cap.** The listing truncates description + `when_to_use` together, so
  the overflow never reaches the activation decision at all. Cut the least discriminating
  sentence, not the trigger list.
- **L3, lifecycle wording.** Move `deprecated` and its relatives out of the description and
  into `status:`. A description that opens by saying the skill is obsolete still spends its
  catalogue cost on every session and still competes for activation.
- **L11, an internal skill without the flag.** Set `user-invocable: false`. Note what it does
  and does not do: it hides the skill from the `/` menu; it does not stop the Skill tool
  reaching it. If the intent is that the model must not start it, that is
  `disable-model-invocation: true`, which is a different key.
- **L23, two skills claiming one phrase.** This is a design question, not a wording tweak.
  Either the two skills should be one, or the boundary between them is real and neither
  description states it. Write the boundary into both: each names the other and says when to
  go there instead. If the copies are deliberate, declare it with `canonical`/`duplicate_of`
  and the rule goes quiet.

## Something is in the wrong layer — L5, L6, L9, L19

- **L6, deterministic work in prose.** Move the arithmetic, the threshold decision or the
  transform into `scripts/`, with a test beside it, and leave the body saying which script to
  run and how to read its output. If the wording is genuinely about judgement rather than
  calculation, suppress it *with the reason*. Do not delete the sentence to silence the rule.
- **L9, scripts with no test.** The script is the half of a skill that can be verified for
  free; a script nobody tested is prose with extra steps. Add `scripts/test_*.py` or a
  `tests/` directory.
- **L19, body too large.** Two different repairs. Knowledge that is only needed on some
  branches goes to `references/` — it costs nothing until it is read. Procedure that is really
  deterministic goes to `scripts/`. Trimming words rarely gets a large body under the line.
- **L5, an unconventional directory.** Rename to `references/` (plural), `scripts/` or
  `assets/`. Update every path in the body at the same time, or L22 will catch the leftovers.

## A declaration that acts on nothing — L8, L18, L20

The common failure: a `requires:` line that reads like a guard but starts nothing, because
the runtime does not interpret it.

- **L18, declared but never invoked.** Add the imperative step to the body: "invoke
  `<guard>` with the Skill tool before doing any work". The declaration records the intent;
  this line is the part that acts.
- **L8, risk wording with no guard.** If the org has a guard for that tier, declare it *and*
  write the invocation step. **If it does not, ask.** Inventing a plausible-looking name
  produces a declaration that protects nothing and looks like it does, which is worse than the
  original finding.
- **L8, external publication with no confirmation.** Add the human confirmation step to the
  Contract's Preconditions, and make sure the body actually waits for it. This is the one
  risk tier whose check needs no org-specific configuration, so it applies everywhere.
- **L20, spend-incurring work the model can start on its own.** Set
  `disable-model-invocation: true`. Two exemptions are already handled and should not be
  worked around: a `flow.json` delegation target (the flag would stop its orchestrator calling
  it) and a configured guard skill (it describes the risk without performing it).

## A name or path that does not resolve — L1, L14, L16, L21, L22, L24

All silent failures: nothing errors at run time, the model simply cannot reach what it was
told to reach.

- **L1, name and directory disagree.** Rename the directory to match the frontmatter, or the
  other way round — but check for references to the old name first, since renaming is what
  creates the rest of this group.
- **L14, one name in two plugins.** Decide which is canonical and declare it on the other with
  `canonical:`. Two skills answering to one name is a coin toss at activation time.
- **L16, a flow delegating to a skill that does not exist.** Either the phase target was
  renamed, or the skill was never installed in this tree. The Atlas draws the break in red at
  the phase where the pipeline stops, which is usually faster than reading the JSON.
- **L21, a dangling `requires`/`depends_on`.** Same cause. If the audit covered only part of
  the estate, this can be a legitimate cross-plugin reference — check the scope of the run
  before treating it as a defect, and suppress with that reason if so.
- **L22, a link into `references/` or `scripts/` that does not resolve.** Fix the link or
  restore the file. Note the rule only follows markdown links into the skill's own
  directories, so a finding here is a real local claim, not a mention in passing.
- **L24, an artifact nobody consumes.** Either a later phase is missing it from its `inputs`
  (declare it), or the step that needed it is gone and the phase is now dead weight (remove
  it). If the phase deliberately ends a branch, suppress with that reason.

## A file that exists but is malformed — L0, L12, L13, L15, L25

Having written the file, get it right. Absence is tolerated in every one of these; a broken
version is not.

- **L0, unparseable frontmatter.** Fix this first — with the frontmatter broken, every other
  rule is skipped for that skill, so a clean-looking report says nothing about it.
- **L12, non-standard keys.** Either it is a typo of an official key, or it is a private
  convention that belongs under `metadata:`. Check the runtime profile before acting: under a
  specification-only profile the Claude Code extensions are non-standard by definition.
- **L13, eval schema.** The two official shapes are `evals/evals.json`
  (`skill_name`, `evals[{id, prompt, expected_output}]`) and `evals/eval_queries.json`
  (`[{query, should_trigger}]`). Legacy `cases.json` gets migrated, not tolerated.
- **L15, flow schema.** Every element needs `phase`, `skill`, `inputs`, `outputs`, with phases
  unique and ascending. Until this passes, the ordering and reachability rules stay silent —
  so a schema fix usually reveals more findings rather than fewer.
- **L25, recorded eval results.** A failing case is not a lint defect to be edited away: it is
  the eval doing its job. Fix the skill, re-run through `skill-creator`, and record the new
  outcome. Deleting `results.json` to reach tier 3 is the one repair that is always wrong.

## Copies and leftovers — L7, L10

- **L7, byte-identical reference files.** Decide which reading is true. If one is the source,
  declare `canonical`/`duplicate_of` on the copies. If it is a managed fan-out, add the
  generated banner — an `AUTO-GENERATED` marker *and* the regeneration command — so the copy
  is machine-traceable to its source. If it is neither, the files have simply not drifted
  apart yet; merge them.
- **L10, a build artifact in the tree.** Delete it and add the pattern to `.gitignore`. It is
  found by walking the working tree rather than by asking git, so an untracked archive counts
  — which is exactly the case that slips through review.

## The Contract — L4

- **A missing Contract on an old skill** is a warning; write one when you next touch the skill.
- **A Contract that exists without Non-goals is an error**, and deliberately so. Non-goals is
  the entry that stops a skill growing into its neighbours, and it is the one people leave
  out. Write what the skill will not do and which skill owns that instead — the second half is
  what makes it useful.
- **A Contract that is not the first heading** buries the interface behind prose. Move it up.

## Ordering — L17

An input consumed at or before the phase that produces it. Either the phases are numbered
wrongly, or the artifact name is reused for two different things. Renaming the second one is
usually the honest fix.
