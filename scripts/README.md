# scripts

Maintenance tooling for this repository. Nothing here ships with the binary, and nothing
here is needed to use mekiki — `go build` remains the whole build.

## Regenerating the README screenshots

The images in `docs/images/` are committed, so they must never be rendered from a real
corpus: an Atlas page embeds every description in the estate it was built from. Two steps
keep that safe.

```bash
# 1. Export your own corpus (this file is gitignored and stays local)
mekiki atlas path/to/skills --out tmp/skill-atlas.html

# 2. Replace every human-readable string with wording from a fictional domain,
#    keeping the shape of the estate — counts, dependency graph, findings, flows
python3 scripts/demo-corpus.py tmp/skill-atlas.html tmp/skill-atlas-demo.html

# 3. Render docs/images/atlas-{board,flow,catalog,runbook}.png from the demo page
python3 scripts/screenshots.py tmp/skill-atlas-demo.html
```

Step 2 refuses to write a page that still contains a CJK character, which is the tripwire
for wording that escaped substitution. Open the demo page and read it before publishing
anything rendered from it — the tripwire catches the source language, not a name that
happens to be English.

Step 3 needs Google Chrome (set `MEKIKI_CHROME` if it lives elsewhere) and Pillow
(`pip install pillow`). It measures each crop box from the live DOM, so a template change
moves the boundary rather than cutting a card in half, and it renders at 2x before
downsampling so the text stays crisp.

Both scripts are deterministic: the same export produces the same page and the same
images.
