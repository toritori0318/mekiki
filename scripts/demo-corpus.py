#!/usr/bin/env python3
"""Turn a real skill-atlas export into a screenshot-safe demo page.

The shape of the estate is preserved — skill count per plugin, the dependency graph, which
skills carry which findings, which skills are orchestrators — while every human-readable
string is replaced with wording from a different domain (a fictional utility
field-operations company). Nothing from the source corpus survives except numbers and
structure, and the script asserts that no CJK character reaches the output.

Three deliberate departures from a literal 1:1 substitution, because the point is a
screenshot of what mekiki can show:

  * a share of the skills are given a complete Contract (their `no ## Contract block`
    finding is dropped and the tier recomputed with the real tier rules), so the tier
    distribution is not a flat wall of T0;
  * a share are given evals, so the top of the tier scale is populated;
  * the first orchestrator becomes a *declared* flow with inputs and outputs, so the Flow
    view shows artifact edges, the artifact ledger and a missing delegate; the rest stay
    inferred, which is what a Phase Registry table really looks like.

Deterministic: same input, same output, no randomness.

    mekiki atlas <your corpus> --out tmp/skill-atlas.html      # never committed
    python3 scripts/demo-corpus.py tmp/skill-atlas.html tmp/skill-atlas-demo.html
    python3 scripts/screenshots.py tmp/skill-atlas-demo.html   # writes docs/images/

Plugins are matched positionally — the largest plugin in the export gets the demo plugin
with the largest name pool — so no name from the source corpus appears in this file. If an
export has more plugins, or a plugin with more skills than a pool can name, the script says
which pool to grow rather than emitting `foo-2` names.
"""

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
TEMPLATE = ROOT / "internal" / "atlas" / "template.html"

# ---------------------------------------------------------------- naming

# Verbs are stored in base form and inflected below, so both the skill name (gerund) and
# the description (third person) come out of the same word.
DOUBLE = {"flag", "tag", "log", "trim", "plan", "map", "run", "submit", "cancel", "label"}


def gerund(v: str) -> str:
    if v in DOUBLE:
        return v + v[-1] + "ing"
    if v.endswith("e") and not v.endswith(("ee", "ye", "oe")):
        return v[:-1] + "ing"
    return v + "ing"


def third(v: str) -> str:
    if v.endswith(("s", "x", "z", "ch", "sh")):
        return v + "es"
    if v.endswith("y") and v[-2] not in "aeiou":
        return v[:-1] + "ies"
    return v + "s"


THEMES = {
    "field-toolkit": [
        ("work-order", ["draft", "dispatch", "close", "validate", "reopen"]),
        ("outage-report", ["draft", "summarise", "escalate", "publish", "archive"]),
        ("meter-reading", ["collect", "validate", "reconcile", "import", "flag"]),
        ("service-window", ["plan", "book", "shift", "confirm", "cancel"]),
        ("crew-roster", ["build", "balance", "publish", "audit", "freeze"]),
        ("route-plan", ["draft", "optimise", "resequence", "verify"]),
        ("safety-checklist", ["issue", "verify", "audit", "revise"]),
        ("permit-request", ["prepare", "submit", "track", "renew"]),
        ("asset-register", ["update", "audit", "reconcile", "export"]),
        ("site-survey", ["schedule", "record", "summarise", "review"]),
        ("fault-code", ["classify", "trace", "rank", "map"]),
        ("inspection-photo", ["collect", "label", "review", "archive"]),
        ("customer-callback", ["schedule", "draft", "log", "escalate"]),
        ("downtime-log", ["collect", "summarise", "chart", "export"]),
        ("spare-part", ["reserve", "order", "substitute", "return"]),
        ("truck-stock", ["count", "replenish", "reconcile", "forecast"]),
        ("job-estimate", ["draft", "revise", "approve", "compare"]),
        ("field-note", ["capture", "clean", "summarise", "tag"]),
        ("service-contract", ["read", "renew", "flag", "summarise"]),
        ("meter-swap", ["plan", "record", "verify", "reverse"]),
        ("incident-brief", ["draft", "circulate", "close", "review"]),
        ("compliance-pack", ["assemble", "check", "submit", "archive"]),
        ("training-record", ["update", "audit", "remind", "export"]),
        ("dispatch-board", ["refresh", "rebalance", "annotate", "freeze"]),
        ("callout-fee", ["calculate", "invoice", "waive", "audit"]),
        ("weather-hold", ["evaluate", "declare", "lift", "log"]),
        ("access-code", ["issue", "rotate", "revoke"]),
        ("no-access-visit", ["record", "reschedule", "escalate"]),
        ("job-photo-set", ["assemble", "compress", "publish"]),
        ("customer-consent", ["capture", "verify", "withdraw"]),
        ("van-checklist", ["issue", "verify", "archive"]),
        ("shift-swap", ["request", "approve", "log"]),
    ],
    "depot-toolkit": [
        ("stock-count", ["run", "reconcile", "adjust", "schedule"]),
        ("goods-receipt", ["record", "match", "dispute", "archive"]),
        ("bin-location", ["assign", "audit", "remap", "print"]),
        ("fleet-check", ["run", "log", "escalate", "schedule"]),
        ("fuel-card", ["issue", "audit", "block", "reconcile"]),
        ("tool-loan", ["issue", "return", "chase", "audit"]),
        ("pallet-label", ["print", "verify", "reprint"]),
        ("supplier-order", ["draft", "chase", "receive", "cancel"]),
        ("depot-rota", ["build", "publish", "audit"]),
    ],
    "intake-toolkit": [
        ("service-request", ["capture", "qualify", "route", "close"]),
        ("site-profile", ["build", "enrich", "verify"]),
        ("quote", ["draft", "revise", "send", "track"]),
        ("handover-pack", ["assemble", "review", "deliver"]),
        ("intake-call", ["transcribe", "summarise"]),
    ],
    "kodama": [
        ("shift-digest", ["compose", "send", "archive"]),
        ("standup-note", ["collect", "summarise", "post"]),
        ("alert-rule", ["draft", "tune", "silence"]),
        ("oncall-handoff", ["prepare", "confirm", "log"]),
    ],
    "field-depot-bridge": [
        ("parts-forecast", ["build", "reconcile", "publish"]),
        ("van-manifest", ["generate", "verify", "amend"]),
        ("depot-request", ["raise", "track", "close"]),
        ("return-note", ["issue", "match"]),
    ],
    "ops-utils": [
        ("run-log", ["rotate", "trim"]),
        ("id-token", ["mint", "revoke"]),
        ("date-window", ["resolve"]),
    ],
}

# Two orchestrators exist in the source; give them names, wording and a Contract that read
# like a pipeline rather than like a single-purpose skill.
WORKFLOW_NAMES = ["intake-workflow", "resurvey-workflow"]
WORKFLOW_DESC = {
    "intake-workflow": (
        "Runs a new service request through the whole intake pipeline, from the first "
        "customer call to the handover pack the crew takes on site. Use this when a "
        "request arrives and the entire sequence should run, or when a stalled request "
        "has to restart from a named phase. Each phase can also be run on its own."
    ),
    "resurvey-workflow": (
        "Re-runs an existing site through survey and planning after something changed on "
        "the ground — a meter swap, a failed visit, a revised permit. Use this when the "
        "site profile is stale rather than missing. If the site has never been surveyed, "
        "use intake-workflow instead of this skill."
    ),
}
WORKFLOW_CONTRACT = {
    "Trigger": "a service request needs the full pipeline, or a named phase re-run.",
    "Inputs": "required: work_order_id. optional: from_phase (defaults to phase 1).",
    "Preconditions": "the operations warehouse holds yesterday's export and the site is "
                     "not under a weather hold.",
    "Outputs": "out/{name}/{{YYYYMMDD}}_{{site}}/ — one numbered artifact per phase",
    "Postconditions": "every phase reports done, and the handover pack exists.",
    "Non-goals": "does not do the work of a phase itself; it only decides what runs and "
                 "in what order.",
}

OPENERS = [
    "{Third} the {noun} for a given site.",
    "{Third} the {noun} so the dispatch desk can act on it the same day.",
    "Produces the {noun} for one site or one crew, ready to review.",
    "Keeps the {noun} in step with what the field actually reported.",
    "Turns the raw records into the {noun} the operations review expects.",
]
TRIGGERS = [
    "Use this when the user asks to {base} a {noun}, or when {situation}.",
    "Use this when {situation}, or when someone asks for the {noun} directly.",
    "Trigger it when the user says \"{base} the {noun}\", or when {situation}.",
]
BOUNDARIES = [
    "If the goal is only to {alt}, do that directly instead of this skill.",
    "It does not {alt}; that belongs to the dispatch board.",
    "Stops at the reviewed file — nothing is sent to the customer from here.",
]
SITUATIONS = [
    "a crew reports something on site that does not match the schedule",
    "the morning dispatch review finds a gap",
    "a customer chases an appointment that has slipped",
    "the depot flags a shortage that affects tomorrow's jobs",
    "an audit asks where a number came from",
    "the weekly operations review is being prepared",
    "an engineer cannot close a job in the field app",
    "a regulator requests evidence for a completed visit",
]
ALTERNATIVES = [
    "read the raw records",
    "check a single job by hand",
    "look up one customer",
    "export a spreadsheet",
    "re-run yesterday's report",
]
FILLER = (
    " It reads only what the operations warehouse already publishes and writes nothing back "
    "to the field system, so the outcome is a file you review before anything leaves the "
    "building. Written for the regional dispatch desks, whose day starts before the first "
    "van does."
)

# Quoted heuristic hits, translated into the demo's language.
WORDS = {
    "投稿": "post", "公開": "publish", "配信": "deliver", "送信": "send", "掲載": "list",
    "計算": "calculate", "算出": "compute", "集計": "aggregate", "換算": "convert",
    "請求": "invoice", "課金": "billing", "決済": "payment", "支払": "pay",
    "削除": "delete", "上書き": "overwrite", "非推奨": "deprecated", "廃止": "retired",
    "実験": "experimental", "ベータ": "beta",
}
CJK = re.compile(r"[぀-ヿ一-鿿ａ-ｚＡ-Ｚ０-９　、。「」（）]+")


def translate_words(text: str) -> str:
    for ja, en in WORDS.items():
        text = text.replace(ja, en)
    return CJK.sub("the flagged wording", text)


def name_pools():
    """Verb x noun, round-robin over nouns so the board is not ten `drafting-*` in a row."""
    pools = {}
    for plugin, groups in THEMES.items():
        names, i = [], 0
        while True:
            added = False
            for noun, verbs in groups:
                if i < len(verbs):
                    names.append((verbs[i], noun))
                    added = True
            if not added:
                break
            i += 1
        pools[plugin] = names
    return pools


def plugin_map(skills, pools):
    """Match source plugins to demo plugins by size, largest first.

    Positional on purpose: a table of real plugin names would put the shape of the private
    corpus into a file that ships publicly.
    """
    counts = {}
    for s in skills:
        counts[s["plugin"]] = counts.get(s["plugin"], 0) + 1
    source = sorted(counts, key=lambda p: (-counts[p], p))
    demo = sorted(pools, key=lambda d: (-len(pools[d]), d))
    if len(source) > len(demo):
        raise SystemExit(f"the export has {len(source)} plugins but THEMES defines "
                         f"{len(demo)} — add another plugin to THEMES")
    mapping = dict(zip(source, demo))
    for src, dst in mapping.items():
        if counts[src] > len(pools[dst]):
            raise SystemExit(f"{dst} must name {counts[src]} skills but its pool holds "
                             f"{len(pools[dst])} — add nouns or verbs to THEMES[{dst!r}]")
    return mapping


def build_names(skills):
    pools = name_pools()
    plugins = plugin_map(skills, pools)

    used, mapping, parts = {p: 0 for p in pools}, {}, {}
    orchestrators = iter(WORKFLOW_NAMES)
    for s in sorted(skills, key=lambda x: x["key"]):
        plugin = plugins[s["plugin"]]
        if s["is_orchestrator"]:
            name = next(orchestrators)
            mapping[s["key"]] = (plugin, name)
            parts[s["key"]] = ("run", "workflow")
            continue
        verb, noun = pools[plugin][used[plugin]]
        used[plugin] += 1
        mapping[s["key"]] = (plugin, f"{gerund(verb)}-{noun}")
        parts[s["key"]] = (verb, noun)
    return mapping, parts


def describe(verb: str, noun: str, i: int, long: bool) -> str:
    noun_text = noun.replace("-", " ")
    d = " ".join([
        OPENERS[i % len(OPENERS)].format(Third=third(verb).capitalize(), noun=noun_text),
        TRIGGERS[i % len(TRIGGERS)].format(
            base=verb, noun=noun_text, situation=SITUATIONS[i % len(SITUATIONS)]),
        BOUNDARIES[i % len(BOUNDARIES)].format(alt=ALTERNATIVES[i % len(ALTERNATIVES)]),
    ])
    if long:
        d += FILLER
    return d


# ---------------------------------------------------------------- rewriting

CONTRACT_TEXT = {
    "Trigger": "the dispatch desk asks for the {noun} of a named site.",
    "Inputs": "required: site_id, service_date. optional: crew_id (defaults to the rostered crew).",
    "Preconditions": "the operations warehouse holds yesterday's export "
                     "(`scripts/check_export.py` exits 0).",
    "Outputs": "out/{name}/{{YYYYMMDD}}_{{site}}/01_{short}.md",
    "Postconditions": "01_{short}.md exists and lists every job it covered.",
    "Non-goals": "does not contact the customer and does not move the schedule "
                 "(the dispatch board owns both).",
}


def contract_for(name: str, noun: str) -> dict:
    if name in WORKFLOW_DESC:
        return {k: v.format(name=name) for k, v in WORKFLOW_CONTRACT.items()}
    short = noun.replace("-", "_")
    return {k: v.format(noun=noun.replace("-", " "), name=name, short=short)
            for k, v in CONTRACT_TEXT.items()}


HEUR_TEXT = {
    "Inputs": ["- site_id (given by the dispatch desk)", "- service_date (defaults to today)"],
    "Outputs": ["- out/01_summary.md — one row per job, with the source record id"],
    "Preconditions": ["- the nightly export has completed"],
}


def rewrite_findings(skill, name_of, has_contract, desc_len):
    out = []
    for f in skill["findings"]:
        rule, msg = f["rule"], f["message"]
        if rule == "L4" and has_contract:
            continue  # this skill now has a Contract, so the finding must go
        for real, demo in name_of.items():
            msg = msg.replace(real, demo)
        msg = msg.replace("skip_schema_check_regression.json", "regression_cases.json")
        msg = translate_words(msg)
        if rule == "L2" and "description is" in msg:
            msg = re.sub(r"description is \d+ characters",
                         f"description is {desc_len} characters", msg)
        out.append({"rule": rule, "severity": f["severity"], "message": msg})
    return out


def tier_of(findings, contract_complete, has_evals):
    """The same cumulative rule as internal/atlas.tierOf, applied to the demo data."""
    hit = {f["rule"] for f in findings}
    err = {f["rule"] for f in findings if f["severity"] == "error"}
    if {"L1", "L2", "L3"} & err or not contract_complete:
        return 0
    if hit & {"L6", "L8", "L9", "L18", "L20"}:
        return 1
    if not has_evals or "L13" in err:
        return 2
    return 3


DECLARED_FLOW = [
    ("capturing-service-request", ["work_order_id"], ["01_intake.json"]),
    ("building-site-profile", ["01_intake.json"], ["02_site_profile.md"]),
    ("scheduling-site-survey", ["01_intake.json", "02_site_profile.md"], ["03_survey_slot.json"]),
    ("drafting-quote", ["02_site_profile.md", "03_survey_slot.json"], ["04_quote.md"]),
    ("assembling-handover-pack", ["04_quote.md"], ["05_handover.pdf"]),
    ("archiving-intake-run", ["05_handover.pdf"], ["run.json"]),   # deliberately not installed
]

INFERRED_OUTPUTS = [
    ["baseline scan → r0_baseline.json + a summary for review"],
    ["baseline + new findings → site_profile_result", "meter_history.json"],
    ["site_profile_result + last plan → revised service plan"],
    ["service plan → coverage_gaps.json (required when R2 and R4 both run)"],
    ["plan section E → schedule.xlsx", "kpi_sheet.xlsx"],
    ["schedule → wbs/kickoff_wbs.json (the previous WBS is kept)"],
    ["one payload per executed block → review → submit → verify"],
]


def main(src: str, dst: str) -> None:
    html = Path(src).read_text()
    start = html.index("const DATA = ") + len("const DATA = ")
    end = html.index(";\n", start)
    data = json.loads(html[start:end])
    skills = data["skills"]

    mapping, parts = build_names(skills)

    # L14 reports the same name in two plugins; keep that true in the demo.
    for s in skills:
        for f in s["findings"]:
            if f["rule"] == "L14":
                keys = re.findall(r"[a-z0-9-]+:[a-z0-9-]+", f["message"])
                if len(keys) >= 2 and all(k in mapping for k in keys[:2]):
                    mapping[keys[1]] = (mapping[keys[1]][0], mapping[keys[0]][1])
                    parts[keys[1]] = parts[keys[0]]

    key_of = {k: f"{p}:{n}" for k, (p, n) in mapping.items()}
    name_of = dict(key_of)
    for k, (_, n) in mapping.items():
        name_of.setdefault(k.split(":", 1)[1], n)
    # Longest first, so a short name does not eat a longer one it is contained in.
    name_of = dict(sorted(name_of.items(), key=lambda kv: -len(kv[0])))

    declared_key = sorted(s["key"] for s in skills if s["is_orchestrator"])[0]

    for i, s in enumerate(skills):
        plugin, name = mapping[s["key"]]
        verb, noun = parts[s["key"]]
        long_desc = any(f["rule"] == "L2" and "characters" in f["message"] for f in s["findings"])
        desc = WORKFLOW_DESC.get(name) or describe(verb, noun, i, long_desc)

        # Roughly three in five skills have adopted the Contract; one in five has evals.
        has_contract = (i % 5) < 3
        if i % 5 == 0:
            s["has_evals"] = True

        s["plugin"], s["name"], s["key"] = plugin, name, f"{plugin}:{name}"
        s["desc"] = desc
        s["findings"] = rewrite_findings(s, name_of, has_contract, len(desc))
        s["errors"] = sum(1 for f in s["findings"] if f["severity"] == "error")
        s["warns"] = len(s["findings"]) - s["errors"]
        s["contract"] = {"present": has_contract, "complete": has_contract,
                         "items": contract_for(name, noun) if has_contract else {}}
        s["heur"] = {} if has_contract else dict(list(HEUR_TEXT.items())[: 1 + (i % 3)])
        s["canonical"] = name_of.get(s["canonical"], "") if s["canonical"] else ""
        for field in ("requires", "depends_on", "mentions"):
            s[field] = [name_of.get(v, v) for v in s[field]]
        s["depended_by"] = sorted({key_of.get(v, v) for v in s["depended_by"]})
        s["tier"] = tier_of(s["findings"], has_contract, s["has_evals"])

    installed = {s["name"] for s in skills}
    for s in skills:
        if not s["is_orchestrator"]:
            continue
        if s["key"] == key_of[declared_key]:
            s["flow_source"] = "declared"
            s["flow"] = [{"phase": n + 1, "skill": sk, "inputs": ins, "outputs": outs}
                         for n, (sk, ins, outs) in enumerate(DECLARED_FLOW)]
            unexpected = [sk for sk, _, _ in DECLARED_FLOW[:-1] if sk not in installed]
            if unexpected:
                raise SystemExit(f"declared flow names skills that do not exist: {unexpected}")
        else:
            for n, step in enumerate(s["flow"]):
                step["skill"] = name_of.get(step["skill"], step["skill"])
                step["inputs"] = []
                step["outputs"] = INFERRED_OUTPUTS[n % len(INFERRED_OUTPUTS)]

    data["notes"] = ["demo corpus: every name and description on this page is fictional"]

    payload = json.dumps(data, ensure_ascii=False)
    Path(dst).write_text(TEMPLATE.read_text().replace("/*__DATA__*/null", payload, 1))

    # Only the payload is checked: the template itself carries a few CJK characters on
    # purpose (the artifact-name normaliser handles Japanese punctuation).
    leaked = CJK.findall(payload)
    if leaked:
        raise SystemExit(f"source wording survived into the output: {leaked[:5]}")
    tiers = {t: sum(1 for s in skills if s["tier"] == t) for t in range(4)}
    print(f"wrote {dst}")
    print(f"  {len(skills)} skills / {len({s['plugin'] for s in skills})} plugins, "
          f"tiers {tiers}, "
          f"{sum(s['errors'] for s in skills)} errors, {sum(s['warns'] for s in skills)} warnings")
    print("  no source wording left on the page")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "tmp/skill-atlas-demo.html")
