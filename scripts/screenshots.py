#!/usr/bin/env python3
"""Render the README screenshots from a demo Atlas page.

    python3 scripts/demo-corpus.py <a real export> tmp/skill-atlas-demo.html
    python3 scripts/screenshots.py tmp/skill-atlas-demo.html

Never point this at a real corpus export: the images are committed, and a real page
carries every description in the estate. `scripts/demo-corpus.py` exists to produce the
safe input.

Two things this does that a manual screenshot does not:

  * renders at 2x and downsamples, so the text stays crisp on both kinds of display;
  * measures the crop box from the live DOM instead of guessing pixel offsets, so a
    layout change cannot silently cut a card in half — it just moves the boundary.

Requires Google Chrome and Pillow (`pip install pillow`). Override the browser with
MEKIKI_CHROME=/path/to/chrome.
"""

import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    from PIL import Image
except ImportError:  # pragma: no cover - a developer-tool dependency, not a runtime one
    sys.exit("Pillow is required: pip install pillow")

REPO = Path(__file__).resolve().parent.parent
DEFAULT_SRC = REPO / "tmp" / "skill-atlas-demo.html"
OUT = REPO / "docs" / "images"
CHROME = os.environ.get(
    "MEKIKI_CHROME", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")

# name: (what to open, JS returning the crop box [x0,y0,x1,y1] in CSS px, window height)
SHOTS = {
    "atlas-board": (
        'setTab("board");',
        """
        const sec=document.querySelector(".psec");
        const tiles=[...sec.querySelectorAll(".tile")];
        const t=tiles[Math.min(tiles.length,18)-1].getBoundingClientRect();
        return [0,0,document.documentElement.clientWidth,Math.round(t.bottom+5)];
        """,
        1500,
    ),
    "atlas-flow": (
        'wfKey=WORKFLOWS.find(w=>w.flow_source==="declared").key; setTab("flow");',
        """
        const c=document.querySelector(".controls").getBoundingClientRect();
        const v=document.getElementById("fcanvas").getBoundingClientRect();
        return [0,Math.round(c.top),document.documentElement.clientWidth,Math.round(v.bottom+14)];
        """,
        1500,
    ),
    "atlas-catalog": (
        'setTab("cat");',
        """
        const c=document.querySelector(".controls").getBoundingClientRect();
        const rows=[...document.querySelectorAll("#rows tr.row")];
        const r=rows[Math.min(rows.length,14)-1].getBoundingClientRect();
        return [0,Math.round(c.top),document.documentElement.clientWidth,Math.round(r.bottom+1)];
        """,
        1500,
    ),
    "atlas-runbook": (
        'wfKey=WORKFLOWS.find(w=>w.flow_source==="declared").key; setTab("flow");',
        """
        const rows=[...document.querySelectorAll(".prow")];
        const top=rows.find(r=>r.textContent==="runbook").getBoundingClientRect().top;
        const steps=[...document.querySelectorAll(".rbstep")];
        const b=steps[Math.min(steps.length,2)-1].getBoundingClientRect().bottom;
        const m=document.getElementById("wfmain").getBoundingClientRect();
        return [Math.round(m.left-14),Math.round(top+2),Math.round(m.right+14),Math.round(b+8)];
        """,
        2200,
    ),
}

WIDTH = 1600


def chrome(page: Path, *args: str) -> str:
    if not Path(CHROME).exists():
        sys.exit(f"Chrome not found at {CHROME} (set MEKIKI_CHROME)")
    done = subprocess.run([CHROME, "--headless=new", "--disable-gpu", *args, str(page)],
                          capture_output=True, text=True)
    return done.stdout


def main(src: Path) -> None:
    if not src.exists():
        sys.exit(f"{src} does not exist — run scripts/demo-corpus.py first")
    html = src.read_text()
    OUT.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="mekiki-shots-"))

    for name, (setup, measure, height) in SHOTS.items():
        # Pass 1: open the view and report the crop box.
        probe = work / f"probe-{name}.html"
        probe.write_text(html.replace("</body>", (
            f'<script>setTimeout(()=>{{ {setup} setTimeout(()=>{{'
            f' document.title=JSON.stringify((()=>{{{measure}}})()); }},350); }},150);</script>'
            "</body>")))
        dom = chrome(probe, "--virtual-time-budget=8000",
                     f"--window-size={WIDTH},{height}", "--dump-dom")
        found = re.search(r"<title>(.*?)</title>", dom)
        if not found:
            sys.exit(f"{name}: the page did not report a crop box")
        box = json.loads(found.group(1))

        # Pass 2: the same view, captured at 2x.
        page = work / f"page-{name}.html"
        page.write_text(html.replace(
            "</body>", f"<script>setTimeout(()=>{{ {setup} }},150);</script></body>"))
        raw = work / f"{name}.png"
        chrome(page, "--virtual-time-budget=8000", f"--window-size={WIDTH},{height}",
               "--force-device-scale-factor=2", f"--screenshot={raw}")

        im = Image.open(raw)
        x0, y0, x1, y1 = (v * 2 for v in box)
        im = im.crop((x0, y0, min(x1, im.width), min(y1, im.height)))
        # Palette quantisation would halve the file size, but it shifts the error red and
        # the warning amber far enough that the colour stops carrying its meaning.
        im = im.resize((im.width // 2, im.height // 2), Image.LANCZOS)
        dst = OUT / f"{name}.png"
        im.save(dst, optimize=True)
        print(f"{dst.relative_to(REPO)}  {im.width}x{im.height}  {dst.stat().st_size // 1024} KB")


if __name__ == "__main__":
    main(Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_SRC)
