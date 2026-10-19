#!/usr/bin/env python3
"""Turns the panel's variable Google Sans Flex (woff2, OFL) into the static TTF weights the
app bundles: app/assets/fonts. Needs fontTools and brotli."""
from pathlib import Path

from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

root = Path(__file__).resolve().parent.parent
for sub, name in [("latin", "GoogleSansFlex"), ("latin-ext", "GoogleSansFlexExt")]:
    for w in (400, 500, 600, 700):
        font = TTFont(root / f"web/public/fonts/google-sans-flex-{sub}.woff2")
        inst = instancer.instantiateVariableFont(font, {"wght": w})
        inst.flavor = None
        inst.save(root / f"app/assets/fonts/{name}-{w}.ttf")
print("done")
