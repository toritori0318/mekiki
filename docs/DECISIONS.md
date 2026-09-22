# Design decisions

The significant adoptions and rejections behind the conventions and the linter.

The operating rule: **a rule is measured against a real corpus (6 plugins, ~190 skills)
before it ships, and rules that do not earn their keep are not adopted.** A warning that
fires on most skills gets ignored; a rule that fires on nothing is pure cost. Both outcomes
happened here, and both are recorded below.

日本語版: **[DECISIONS.ja.md](DECISIONS.ja.md)**

**On the numbers:** the measured counts cited below (154 to 192 skills, depending on the
entry) are snapshots taken at different points as the corpus grew, and grew monotonically
over the development period. They are not measurement noise — each entry simply records the
corpus size at the time that decision was made.

## Adopted

1. **Context minimality tests (already-known / deletable / action / layer / duplication)**
   — irrelevant content measurably degrades accuracy. Sources: Chroma's "Context Rot"
   (a single distractor degrades retrieval,
   <https://research.trychroma.com/context-rot>), GSM-IC
   (<https://arxiv.org/abs/2302.00093>), "Lost in the Middle"
   (<https://arxiv.org/abs/2307.03172>), and Anthropic's "the smallest possible set of
   high-signal tokens"
   (<https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents>).

2. **Size discipline checks both lines and estimated tokens (L19)** — the official guidance
   is "under 500 lines and 5,000 tokens". Line count alone misses a CJK corpus: measured at
   ~76 characters per line, one corpus had exactly one skill over 500 lines while 93
   exceeded 5,000 characters of body. CJK is estimated at 1.2 tokens per rune, following
   measurements that newer tokenizers consume 15-35% more for CJK than the previous
   generation.

3. **Both official eval formats (`evals/evals.json` and `evals/eval_queries.json`)** — do
   not invent a proprietary schema. An earlier version of this protocol did, and its linter
   then flagged genuinely official files as "non-standard", penalising exactly the skills
   that had got it right. Sources:
   <https://agentskills.io/skill-creation/evaluating-skills> and
   <https://agentskills.io/skill-creation/optimizing-descriptions>.

4. **Eval methodology** — run each case with and without the skill and compare the delta
   (pass rate, time, tokens); measure activation with ~20 queries run three times each,
   split train 60% / validation 40%. The runner is delegated to `skill-creator` rather than
   reimplemented.

5. **`disable-model-invocation: true` on side-effecting skills (L20)** — unlike `requires`,
   this is the one activation control the runtime actually enforces. Mechanical checking is
   scoped to billing: the wording for external publication and destructive writes hit 68% of
   a real corpus, which makes the finding unactionable, so those stay a convention without
   enforcement.

6. **Org-specific values and detection patterns live in config** — guard skill names
   (L8, L20) and detection patterns (L2, L3, L6, L8, L11) are overridable through
   `config.json`. Hardcoding either breaks other organisations outright (a demand to declare
   a skill that does not exist) or silently (heuristics inert outside one language).
   Built-in patterns are bilingual so neither language needs configuration.

7. **Naming enforcement covers only the official constraints (L1)** — enforcing the three
   category shapes collapsed under measurement: all 25 findings were valid official names,
   62% of skills slipped through the "noun, three words" catch-all so the gerund requirement
   never bound, and the problem worth catching (name/directory mismatch) occurred zero
   times. Demoted to SHOULD.

8. **`when_to_use` is an official key (L2, L12)** — it is concatenated onto the description
   and the listing shows the combination up to 1,536 characters
   (<https://code.claude.com/docs/en/skills#frontmatter-reference>). It had been treated as
   non-standard; every one of those 27 warnings was a false positive, and the rule was
   withdrawn.

9. **The skill-creation ladder, delta mode, artifact IR, simplification markers and atomic
   deliver** — ported from archify (<https://github.com/tt-a1i/archify>) and ponytail
   (<https://github.com/DietrichGebert/ponytail>). Details in the conventions document.

10. **Maturity tiers 0-3** — a mekiki-specific scale; no maturity model exists in the
    official specification. Modelled on SLSA's cumulative assurance levels
    (<https://slsa.dev/spec/v1.0/levels>). A single ladder hides progress on one axis behind
    a gap on another, which is why SLSA split into tracks in v1.0; mekiki answers that with
    two layers — the tier as a headline, the Atlas badges as per-axis truth. Supply-chain
    trust for third-party skills (the "verification" the market now means, per Snyk's
    ToxicSkills audit, <https://snyk.io/blog/toxicskills-malicious-ai-agent-skills-clawhub/>)
    is explicitly out of scope.

11. **Go, single binary, subcommands** — distribution was the main adoption barrier for a
    CLI. Verified by differential testing against a 190-skill corpus using mekiki's own diff
    mode as the oracle: errors matched exactly, and the remaining delta was strictly an
    improvement (6 genuine findings the previous implementation missed because it only
    examined `.py` and `.sh`; 2 false positives it had been reporting).

12. **Safety rules favour recall; quality heuristics favour precision** — an asymmetry
    discovered by that same measurement. `risk_*` patterns drive error-severity findings, so
    a miss is worse than a false positive, and word boundaries actively hurt (Go's RE2 counts
    `_` as a word character, so `\bbrowser\b` missed
    `09_browser-operation-notes.md`). The `deterministic` pattern drives warnings a
    human triages, so it is bounded on both sides: an unbounded "ratio" also matches
    generation, iteration and migration (61 false positives), and "comput" matches
    "computer.type", a tool name rather than a calculation.

13. **Reference integrity for links, not for prose (L22)** — the rule began as "every
    path-like string in the body must exist". Measured on a 154-skill corpus it produced 53
    findings and **not one defect**. Two shapes dominate a skill body and neither claims the
    file is local: illustrative examples in skill-authoring guides ("Examples:
    `references/finance.md` for financial schemas") and prose pointers to another skill's
    file ("see querying-warehouse's references/04_RECONCILIATION_GATES.md"). Restricting to
    markdown links under the four conventional directories removes that entire class —
    `<link>` placeholders and `${CLAUDE_SKILL_DIR}/...` variables fall outside it too. The
    narrowed rule covers **1,381 links on the same corpus with none broken**: a check with a
    large healthy subject, which is a different thing from a check with no subject
    (contrast the rejected `name` elaborations below).

14. **Dangling `requires` / `depends_on` (L21) ships without corpus evidence** — the
    measured corpus contains **zero** such declarations, so the rule could not be exercised.
    Adopted anyway, and the distinction from the rejected `name` checks is deliberate: those
    were inert while their subject (a name) was present on every skill, whereas here the
    subject is absent entirely. L21 is the cross-cutting twin of L16, the protocol's own L8
    *demands* the declaration it validates, and a rule over an absent field costs nothing
    and cannot produce a false positive. Revisit if a corpus that uses `requires` ever
    reports noise.

15. **SARIF output, emitted by hand** — findings in a job log make a reviewer go looking for
    the file; SARIF puts them on the line. The subset GitHub consumes is small enough to
    write as plain structs, and a dependency here would contradict the binary's own
    supply-chain discipline. `diff --format sarif` carries **added findings only**, because
    uploading a full lint of an inherited corpus would annotate the backlog on every pull
    request — the same reasoning that produced delta mode in the first place. Per-rule
    descriptions are deliberately omitted from the tool metadata: they would duplicate the
    rule table in DESIGN.md, and a copy that drifts is worse than an absent one.

16. **Activation collisions are detected on quoted phrases only (L23)** — the conventions
    name "skills fire late, or never, or all at once" as a defect of scale, but nothing
    checked the *between-skills* half of it. Whole-description similarity was rejected: it
    needs a threshold nobody can defend and the measurement to set one honestly does not
    exist. A quoted phrase is the author writing down the words a user will say, so two
    authors writing the same words is a claim on one request rather than shared vocabulary.
    Measured on a 154-skill corpus the first version fired 14 times with two noise classes —
    `"analyze"` (a rune floor cannot separate a word from a request, since four runes is a
    whole word in English and a fragment in Japanese) and `"use ${CLAUDE_PLUGIN_ROOT}"` (an
    instruction to an implementer, not an utterance). Requiring a space in a
    space-separated language and excluding `${` leaves **8 findings over 5 skills, every one
    a real collision** — including three channel skills that all claim "how do I set this up".

17. **The unread half of the flow graph (L24)** — L17 caught an input read before it was
    produced; the reverse, an output nothing consumes, was visible in the Atlas artifact
    ledger but was never a finding. Like L21 this ships without corpus evidence: the
    corpus available for measurement contains no `flow.json` at all, so L15, L16, L17 and
    L24 alike rest on fixtures there. The rule cannot fire on a corpus that declares no
    flow, which is what makes adopting it cheap.

18. **Tier 3 reads a recorded eval result, without running anything (L25)** — the tier
    treated the *presence* of both eval files as verification, which a skill can satisfy
    while failing every case in them. Running the evals stays delegated to `skill-creator`
    (decision 4); mekiki reads `evals/results.json` and nothing more, so the boundary is
    unchanged and no runner is reimplemented. Absence is silent so an existing corpus is
    unaffected, and trigger accuracy is recorded but never judged: a pass mark needs a
    threshold, and no measurement here could set one honestly.

19. **Runtime profiles, defaulting to no change (Phase 7)** — the 1,536-character listing
    cap in L2 and the extension keys in L12's allow-list are Claude Code behaviours, not
    rules of the Agent Skills specification. Compiled in, they quietly mis-audit a corpus
    written for a specification-only runtime, where `when_to_use` genuinely is a
    non-standard key and nothing concatenates it onto the description. `profile` selects
    between them and `limits` overrides individual thresholds. The body-size limits are
    **not** runtime-specific — they come from the official guidance — so both profiles
    carry the same values. Acceptance was byte-identical output on a 154-skill corpus with
    no config and with `profile: claude-code`; without that check the refactor would have
    been indistinguishable from a silent behaviour change.

20. **mekiki ships an Agent Skill (`skills/auditing-skill-corpus/`), and the ladder is why
    it is allowed to** — the
    conventions say a new skill is a last resort, so this one had to clear their own ladder
    rather than be waved through by its author. It does: the ordering it encodes is a
    procedure, not a fact (so not a line of agent instructions), it is longer than a
    configuration setting can express, and it has no existing skill to grow into. What made
    it worth writing is that mekiki's finding messages are self-explanatory while the
    *order* of the work is not — and the order is where an agent fails: it lints an
    inherited corpus and starts editing, treats warnings as a to-do list, repairs a prose
    heuristic by deleting the sentence, and invents a guard name when none is configured.
    Repair guidance sits in `references/` for the layer test and for a mechanical reason: it
    must quote the risk vocabulary L8 and L20 detect, and those rules read the description
    and body, so in the body it would trip the rules it explains. A test asserts the skill
    lints clean, which makes it a standing check on the rules themselves.

21. **A second bundled skill for the first contact (`getting-started-with-mekiki`)** — it
    too had to clear the ladder, and the deciding question was why not grow
    `auditing-skill-corpus` instead. Because the two situations pull the Contract in
    opposite directions: a first contact should do everything itself except choose the
    corpus (install, run, summarise — asking a newcomer about baselines is noise), while an
    audit should *stop* at every operating decision (the baseline, a guard's name, whether a
    duplicate is deliberate). One skill serving both either asks too much of a beginner or
    decides too much for an operator, and its description would have to carry both trigger
    vocabularies at once — past the length cap and toward exactly the over-firing the
    conventions warn about. The split is policed mechanically: disjoint quoted trigger
    phrases (L23 runs on every lint of `skills/`), mutual redirection in both descriptions
    and Non-goals, and each skill's eval near-misses aim at the other.

22. **Distribution: a self-bumping tap and a marketplace manifest, no release tooling** —
    decision 11 named distribution as the adoption barrier, and `go install` still assumes a
    Go toolchain. The Homebrew formula lives in a separate tap and builds from the tag
    tarball; its repository **bumps itself** on a schedule by checking the latest release,
    because a repository can push to itself with the default token — the conventional
    arrangement (the release workflow pushing into the tap) needs a cross-repository PAT,
    which is a standing credential this project would rather not hold. Release binaries are
    built by a hand-rolled workflow for the same reason the SARIF writer is hand-rolled: the
    job is a loop around `go build`, and a release tool would be the binary's only
    dependency. The `.claude-plugin/` manifests make the repository double as a plugin
    marketplace, so the bundled skills install as a managed plugin instead of a `cp -r`.

23. **Prose heuristics ignore identifiers** — inline code, link targets, quoted strings and
    file paths are stripped before the prose rules look at a line. A skill name in backticks
    or a filename is a reference, not a description of work; this accounted for every
    remaining L6 false positive on a real corpus.

24. **"Lint this pull request" is a flag on `lint`, not a `mekiki pr` subcommand (`--changed`)** —
    the corpus-level answer already existed as `diff`, and it kept being handed the wrong
    question: `diff` compares two sets of findings, so a pull request that edits a skill
    without altering which rules fire reports `no change` while the skills it touched are
    full of errors. The missing answer was "what state are the touched skills in", which is
    a narrowing of `lint`, not a new verb. A subcommand would also have to re-expose
    `--format`, `--severity`, `--config` and `--baseline`, and `pr` would be a lie: the input
    is a git revision, not a pull-request number. The default base is `origin/HEAD`, so the
    common case needs no argument, which is where the ergonomics of a dedicated verb were
    actually going to come from. If GitHub integration is ever added — resolving a PR number,
    posting the comment — that is when the verb earns its name.

25. **Narrowing applies to the report, never to the evaluation** — the cross-cutting rules
    (duplicated references, a flow naming a skill that does not exist, two skills claiming
    the same trigger) are the ones most worth having on a pull request, and none of them can
    say anything from the changed files alone. So the whole corpus is discovered and every
    rule evaluated, and only the output is scoped. A finding survives when it belongs to a
    touched skill **or when its message names one**, which is what keeps the flow a deletion
    broke visible in a skill the change never opened. Unlike `--severity`, the narrowing does
    move the exit code: a severity filter turning a red build green is a failure mode, while
    a pull request failing on an error it neither introduced nor touched is the failure mode
    this exists to remove.

26. **Judged rules are an opt-in flag on `lint`, in their own package, and never touch the
    exit code (`--jev`)** — the conventions SKILL_PROTOCOL marks *unenforced (review)* are
    claims a skill makes about itself held against what it says elsewhere, which is the one
    class a calibrated classifier is measurably good at and a regular expression is not. But
    a network call, a key and a non-deterministic answer contradict three of this tool's
    constraints at once, so they live behind one flag in `internal/jev`, which `internal/lint`
    never imports; without the flag no socket opens. J findings are warnings, `diff` skips
    them (a verdict near its cutoff moves between runs and would read as an add plus a
    resolve), and `--dry-run` prices a run with no key so what would be sent is visible
    before anything is. The name is the vendor's: the API shape is Jev's, and a neutral
    `--judge` hid that from the reader. A provider-neutral verb earns its name when a second
    provider exists, the same order as `mekiki pr`.

27. **Jev adds and annotates; it never suppresses** — J1 appends its probability to the L6
    finding it judged instead of hiding it, J4 opens the L20 tiers the wording made
    unactionable as warnings, and no rule consults L8. Safety rules favour recall (decision
    12), and a model does not treat adversarial text in its state as adversarial while a
    regular expression cannot be talked out of a match. Cutoffs are fitted on the fixtures
    under `internal/jev/testdata/` by a live run and replayed in CI without a key; the first
    fit put every bad case at 0.87 or above and every clean case at 0.15 or below, so 0.5
    holds for all five. Fixtures are synthetic, so nothing from a
    real corpus enters the repository.

## Rejected

- **Delegating validation to the official `skills-ref`** — it states it is "intended for
  demonstration purposes only" and "not meant to be used in production". Any official
  constraint worth checking is implemented in mekiki instead.

- **Extra `name` checks (64-character limit, consecutive hyphens and so on) as separate
  rules** — zero violations across a real corpus. A rule that catches nothing costs context
  and maintenance and buys nothing. (The constraints are still enforced as part of L1; what
  was rejected was elaborating them further.)

- **A `description` check for unquoted `": "`** (a known cross-client YAML parsing hazard) —
  also zero occurrences in practice.

- **Full specification in OpenAPI, SQLite for master data, bulk rewriting of an existing
  corpus** — a heavy specification becomes a document nobody follows; migrate when the
  simple format actually breaks; replace gradually through staged adoption.

- **Intensity modes for minimalism (lite / full / ultra)** — a strength switch inside the
  conventions destroys the single source of truth.

- **A rendering preview mechanism and update notifications** — out of scope.
