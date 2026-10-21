#!/usr/bin/env python3
"""Rewrites the panel's icon paths from Google's official Material Symbols Rounded.

Each key is the name the panel uses, each value the official symbol it draws:
symbols/web/<symbol>/materialsymbolsrounded/<symbol>_24px.svg (weight 400, grade 0,
optical size 24) from https://github.com/google/material-design-icons. Most glyphs are
drawn on the 960 x 960 grid; a few are still published on the legacy 24 x 24 grid, which
is why the paths are split in two blocks. Both are inlined in web/src/components/Icon.vue
so no icon font has to be downloaded. Run it after adding an icon, then
tools/gen-app-icons.py to copy them into the app.
"""
import re
import urllib.request
from pathlib import Path

SYMBOLS = {
    "overview": "dashboard",
    "assistant": "auto_awesome",
    "send": "arrow_upward",
    "terminal": "terminal",
    "power": "power_settings_new",
    "apps": "rocket_launch",
    "branch": "account_tree",
    "lock": "lock",
    "services": "list_alt",
    "containers": "deployed_code",
    "security": "security",
    "updates": "system_update_alt",
    "settings": "settings",
    "restart": "restart_alt",
    "play": "play_arrow",
    "logs": "subject",
    "search": "search",
    "chevron": "expand_more",
    "chevron-right": "chevron_right",
    "back": "arrow_back",
    "plus": "add",
    "close": "close",
    "check": "check",
    "alert": "warning",
    "info": "info",
    "key": "key",
    "logout": "logout",
    "phone": "smartphone",
    "globe": "public",
    "trash": "delete",
    "external": "open_in_new",
    "copy": "content_copy",
    "sun": "light_mode",
    "history": "history",
    "folder": "folder",
    "person": "person",
    "edit": "edit",
    "database": "database",
    "game": "sports_esports",
    "music": "music_note",
    "film": "movie",
    "chat": "chat_bubble",
    "cloud": "cloud",
    "mail": "mail",
    "cart": "shopping_cart",
    "code": "code",
    "robot": "smart_toy",
    "chart": "bar_chart",
    "camera": "photo_camera",
    "book": "menu_book",
    "home": "home",
    "bolt": "bolt",
    "heart": "favorite",
    "star": "star",
    "leaf": "eco",
    "paw": "pets",
    "coffee": "coffee",
    "image": "image",
    "video": "videocam",
    "bell": "notifications",
    "cpu": "memory",
    "wrench": "build",
    "users": "group",
    "calendar": "calendar_month",
    "map": "map",
    "server": "dns",
    "shop": "storefront",
    "more": "more_horiz",
    "stop": "stop",
}

URL = (
    "https://raw.githubusercontent.com/google/material-design-icons/master"
    "/symbols/web/{0}/materialsymbolsrounded/{0}_24px.svg"
)
GRID_960, GRID_24 = "0 -960 960 960", "0 0 24 24"

root = Path(__file__).resolve().parent.parent
icon_vue = root / "web/src/components/Icon.vue"

paths, small = {}, {}
for name, symbol in SYMBOLS.items():
    try:
        with urllib.request.urlopen(URL.format(symbol), timeout=30) as res:
            svg = res.read().decode()
    except Exception as e:  # noqa: BLE001 - report which symbol failed
        raise SystemExit(f"{symbol} ({name}): {e}")
    # The legacy 24 px files carry no viewBox at all.
    view_box = re.search(r'viewBox="([^"]+)"', svg)
    grid = view_box.group(1).strip() if view_box else GRID_24
    if grid not in (GRID_960, GRID_24):
        raise SystemExit(f"{symbol}: unexpected viewBox {grid}")
    (paths if grid == GRID_960 else small)[name] = re.search(
        r'<path[^>]*\sd="([^"]+)"', svg
    ).group(1)


def block(const, icons, comment=None):
    lines = [f"const {const} = {{"]
    lines += [f"  '{n}': '{icons[n]}'," + (f" // {SYMBOLS[n]}" if comment else "") for n in icons]
    lines.append("} as const")
    return "\n".join(lines)


src = icon_vue.read_text()
src, n = re.subn(
    r"const PATHS = \{.*?\n\} as const", lambda _: block("PATHS", paths, True), src, flags=re.S
)
if n != 1:
    raise SystemExit("could not find the PATHS block in Icon.vue")
small_block = block("SMALL", small, True)
if "const SMALL = {" in src:
    src = re.sub(r"const SMALL = \{.*?\n\} as const", lambda _: small_block, src, flags=re.S)
else:
    src = src.replace("// Brand marks", small_block + "\n// Brand marks", 1)
icon_vue.write_text(src)
print(f"{len(paths)} icons on the 960 grid, {len(small)} on the legacy 24 grid")
