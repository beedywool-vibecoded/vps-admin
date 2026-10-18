#!/usr/bin/env python3
"""Builds the README visuals in .github/assets: banner, section titles and footer in
light and dark versions (Material 3 Expressive, same look as the portfolio), plus the
screenshots as WebP.

    python3 tools/readme-assets.py path/to/screenshots

The screenshot folder holds the PNGs taken by the end-to-end browser tests.
"""
import base64
import io
import math
import os
import sys

from PIL import Image

ROOT = os.path.join(os.path.dirname(__file__), "..", ".github", "assets")

THEMES = {
    "dark": dict(card="#211F26", page="#141218", bar="#2B2930", text="#E6E0E9", muted="#CAC4D0", outline="#938F99",
                 line="#49454F", pc="#4F378B", on_pc="#EADDFF", secc="#4A4458", on_secc="#E8DEF8", tc="#633B48",
                 on_tc="#FFD8E4", dot="#D0BCFF"),
    "light": dict(card="#F3EDF7", page="#FEF7FF", bar="#E6E0E9", text="#1D1B20", muted="#49454F", outline="#79747E",
                  line="#CAC4D0", pc="#EADDFF", on_pc="#21005D", secc="#E8DEF8", on_secc="#1D192B", tc="#FFD8E4",
                  on_tc="#31111D", dot="#6750A4"),
}

SHAPES = {"cookie": (9, 0.07), "clover": (4, 0.17), "sunny": (8, 0.10), "flower": (6, 0.13)}

STYLE = """<style>
  text { font-family: 'Google Sans Flex','Google Sans','Roboto Flex',Roboto,'Segoe UI',system-ui,sans-serif; }
  .spin { transform-box: fill-box; transform-origin: center; animation: spin 48s linear infinite; }
  .float { animation: float 6s ease-in-out infinite; }
  .fade { animation: fade .8s cubic-bezier(.2,0,0,1) backwards; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @keyframes float { 50% { transform: translateY(-8px); } }
  @keyframes fade { from { opacity: 0; transform: translateY(12px); } }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; } }
</style>"""


def shape(kind, cx, cy, r, steps=96):
    lobes, depth = SHAPES[kind]
    pts = []
    for i in range(steps):
        t = 2 * math.pi * i / steps
        rr = r * (1 + depth * math.cos(lobes * t))
        pts.append(f"{cx + rr * math.cos(t):.1f},{cy + rr * math.sin(t):.1f}")
    return "M" + " L".join(pts) + " Z"


def webp_b64(path, width, crop_height=None):
    img = Image.open(path).convert("RGB")
    if crop_height:
        img = img.crop((0, 0, img.width, min(img.height, crop_height)))
    img = img.resize((width, round(img.height * width / img.width)), Image.LANCZOS)
    buf = io.BytesIO()
    img.save(buf, "WEBP", quality=82, method=6)
    return base64.b64encode(buf.getvalue()).decode()


def save_webp(src, dst, width, crop_height=None):
    img = Image.open(src).convert("RGB")
    if crop_height:
        img = img.crop((0, 0, img.width, min(img.height, crop_height)))
    if img.width > width:
        img = img.resize((width, round(img.height * width / img.width)), Image.LANCZOS)
    img.save(dst, "WEBP", quality=84, method=6)


def write(name, svg):
    with open(os.path.join(ROOT, name), "w") as f:
        f.write(svg)


def chips(c, x, y, items):
    out = []
    for label, bg, fg in items:
        w = 26 + len(label) * 8.6
        out.append(f'<rect x="{x:.1f}" y="{y}" width="{w:.0f}" height="40" rx="20" fill="{bg}"/>'
                   f'<text x="{x + w / 2:.0f}" y="{y + 25}" font-size="15" font-weight="500" text-anchor="middle" fill="{fg}">{label}</text>')
        x += w + 8
    return "".join(out)


def banner(theme, shot):
    c = THEMES[theme]
    img = webp_b64(shot, 800, 1000)
    return f"""<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="340" viewBox="0 0 1000 340" role="img">
<title>VPS Admin | Panel auto-hébergé pour serveurs Linux</title>
{STYLE}
<defs>
  <clipPath id="card"><rect width="1000" height="340" rx="36"/></clipPath>
  <clipPath id="win"><rect x="560" y="80" width="400" height="222"/></clipPath>
  <clipPath id="frame"><rect x="560" y="52" width="400" height="250" rx="18"/></clipPath>
</defs>
<g clip-path="url(#card)">
  <rect width="1000" height="340" fill="{c['card']}"/>
  <path class="spin" d="{shape('cookie', 775, 175, 165)}" fill="{c['pc']}"/>
  <path class="spin" d="{shape('flower', 945, 330, 90)}" fill="{c['tc']}"/>
  <path d="{shape('clover', 520, 330, 60)}" fill="{c['secc']}"/>
  <g class="float"><circle cx="958" cy="44" r="13" fill="{c['dot']}"/></g>
</g>
<g class="fade" transform="rotate(-4 760 177)">
  <g clip-path="url(#frame)">
    <rect x="560" y="52" width="400" height="250" fill="{c['page']}"/>
    <rect x="560" y="52" width="400" height="28" fill="{c['bar']}"/>
    <circle cx="576" cy="66" r="5" fill="{c['outline']}"/><circle cx="592" cy="66" r="5" fill="{c['outline']}"/><circle cx="608" cy="66" r="5" fill="{c['outline']}"/>
    <rect x="636" y="59" width="248" height="14" rx="7" fill="{c['page']}"/>
    <text x="760" y="70" font-size="10" text-anchor="middle" fill="{c['muted']}">admin.example.com</text>
    <image href="data:image/webp;base64,{img}" x="560" y="80" width="400" height="222" preserveAspectRatio="xMidYMin slice" clip-path="url(#win)"/>
  </g>
  <rect x="560.5" y="52.5" width="399" height="249" rx="18" fill="none" stroke="{c['line']}"/>
</g>
<g class="fade">
  <rect x="48" y="52" width="216" height="32" rx="8" fill="none" stroke="{c['outline']}"/>
  <text x="62" y="73" font-size="14" font-weight="500" fill="{c['muted']}">pour n'importe quel VPS</text>
  <text x="44" y="166" font-size="72" font-weight="500" letter-spacing="-1.5" fill="{c['text']}">VPS Admin</text>
  <text x="48" y="212" font-size="21" fill="{c['muted']}">Panel, déploiements et assistant pour tes serveurs</text>
  {chips(c, 48, 250, [('Material You', c['pc'], c['on_pc']), ('Go', c['secc'], c['on_secc']), ('Flutter', c['tc'], c['on_tc'])])}
</g>
</svg>
"""


def section(theme, num, title, kind):
    c = THEMES[theme]
    width = 84 + len(title) * 15.5 + 24
    return f"""<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="100" viewBox="0 0 1000 100" role="img">
<title>{title}</title>
{STYLE}
<g transform="translate(0 28)">
<path class="spin" d="{shape(kind, 36, 36, 30)}" fill="{c['pc']}"/>
<text x="36" y="42" font-size="16" font-weight="600" text-anchor="middle" fill="{c['on_pc']}">{num}</text>
<text x="84" y="46" font-size="30" font-weight="500" fill="{c['text']}">{title}</text>
<rect x="{width:.0f}" y="35" width="{1000 - width:.0f}" height="2" rx="1" fill="{c['line']}"/>
</g>
</svg>
"""


def footer(theme):
    c = THEMES[theme]
    blobs = [("cookie", 60, 30, 70, c["pc"]), ("clover", 230, 110, 50, c["secc"]), ("sunny", 770, 10, 60, c["tc"]),
             ("flower", 940, 100, 70, c["pc"]), ("cookie", 600, 125, 40, c["secc"])]
    paths = "".join(f'<path class="spin" d="{shape(k, x, y, r)}" fill="{f}" opacity=".9"/>' for k, x, y, r, f in blobs)
    return f"""<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="120" viewBox="0 0 1000 120" role="img">
<title>VPS Admin</title>
{STYLE}
<defs><clipPath id="card"><rect width="1000" height="120" rx="28"/></clipPath></defs>
<g clip-path="url(#card)">
  <rect width="1000" height="120" fill="{c['card']}"/>
  {paths}
</g>
<text x="500" y="68" font-size="18" font-weight="500" text-anchor="middle" fill="{c['text']}">beedywool.com</text>
</svg>
"""


SECTIONS = [
    ("preview", "01", "Aperçu", "cookie"),
    ("features", "02", "Fonctionnalités", "clover"),
    ("install", "03", "Installation", "sunny"),
    ("security", "04", "Sécurité", "flower"),
    ("stack", "05", "Stack", "cookie"),
    ("dev", "06", "Développement", "clover"),
]

# (source png, output name, width, crop height)
SCREENS = [
    ("03-overview.png", "overview-dark", 1440, 1000),
    ("11-overview-light-teal.png", "overview-light", 1440, 1000),
    ("24-app-after-push.png", "app-detail", 1440, 1000),
    ("22-new-app.png", "new-app", 1440, 1000),
    ("31-assistant-approval.png", "assistant", 1440, 1000),
    ("06-security.png", "security", 1440, 1000),
    ("01-pair.png", "pair", 1440, 1000),
    ("12-mobile.png", "m-overview", 390, 844),
]


def main():
    shots = sys.argv[1] if len(sys.argv) > 1 else "."
    os.makedirs(os.path.join(ROOT, "screens"), exist_ok=True)
    for theme in THEMES:
        write(f"banner-{theme}.svg", banner(theme, os.path.join(shots, "03-overview.png")))
        write(f"footer-{theme}.svg", footer(theme))
        for key, num, title, kind in SECTIONS:
            write(f"section-{key}-{theme}.svg", section(theme, num, title, kind))
    for src, name, width, crop in SCREENS:
        save_webp(os.path.join(shots, src), os.path.join(ROOT, "screens", name + ".webp"), width, crop)
    print("assets written to", os.path.normpath(ROOT))


if __name__ == "__main__":
    main()
