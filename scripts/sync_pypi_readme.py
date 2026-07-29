#!/usr/bin/env python3
"""Mirror the repo README into python/README.md (the PyPI long description).

Relative markdown links/images are rewritten to absolute GitHub URLs so
they render correctly on PyPI. Run from the repo root; CI runs this
before every wheel build so the two can never drift.
"""

import pathlib
import re

REPO = "https://github.com/gizmodata/gizmosql-adbc"
BLOB = f"{REPO}/blob/main"

root = pathlib.Path(__file__).resolve().parent.parent
readme = (root / "README.md").read_text(encoding="utf-8")


def absolutize(match: re.Match) -> str:
    text, target = match.group(1), match.group(2)
    if target.startswith(("http://", "https://", "#", "mailto:")):
        return match.group(0)
    return f"[{text}]({BLOB}/{target})"


synced = re.sub(r"\[([^\]]*)\]\(([^)\s]+)\)", absolutize, readme)
header = (
    "<!-- Generated from the repo README by scripts/sync_pypi_readme.py — do not edit. -->\n\n"
)
(root / "python" / "README.md").write_text(header + synced, encoding="utf-8")
print(f"synced {len(synced)} chars into python/README.md")
