"""Make DevTopo's icons from the logo.

Run from the repository root after changing packaging/logo.png:

    python packaging/make-icons.py

It writes, each scaled from the logo's own pixels (trimmed to its drawing and
centred on a clear square), so every size is sharp rather than scaled by the
system:

- internal/launch/icons/{16,24,32,48,64}.png  the tray icon (all sizes go
  into the Windows icon; macOS and Linux take the 64 px one)
- ui/public/favicon.ico, favicon-16.png, favicon-32.png  the browser tab
- ui/src/assets/logo-64.png  the page header (shown at 28 px, sharp on
  high-resolution screens)

Needs Pillow (pip install pillow).
"""
from pathlib import Path

from PIL import Image, ImageFilter

root = Path(__file__).resolve().parent.parent
logo = Image.open(root / "packaging" / "logo.png").convert("RGBA")

# Trim to the drawing and centre it on a clear square.
logo = logo.crop(logo.getbbox())
side = max(logo.size)
square = Image.new("RGBA", (side, side), (0, 0, 0, 0))
square.paste(logo, ((side - logo.width) // 2, (side - logo.height) // 2))


def size(px: int) -> Image.Image:
    img = square.resize((px, px), Image.LANCZOS)
    # A touch of sharpening keeps the eyes and edges crisp at the smallest sizes.
    if px <= 32:
        img = img.filter(ImageFilter.UnsharpMask(radius=0.6, percent=60, threshold=2))
    return img


icons = root / "internal" / "launch" / "icons"
icons.mkdir(exist_ok=True)
for px in (16, 24, 32, 48, 64):
    size(px).save(icons / f"{px}.png", optimize=True)

public = root / "ui" / "public"
public.mkdir(exist_ok=True)
size(16).save(public / "favicon-16.png", optimize=True)
size(32).save(public / "favicon-32.png", optimize=True)
size(48).save(public / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])

assets = root / "ui" / "src" / "assets"
assets.mkdir(exist_ok=True)
size(64).save(assets / "logo-64.png", optimize=True)

print("icons written")
