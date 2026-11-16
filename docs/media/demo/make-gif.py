"""Turn the frames record.js wrote into a GIF.

    python make-gif.py <frames-dir> <out.gif> [width]

Without a width the frames keep their recorded size, which is sharpest.

Each frame lasts until the next one was taken, so the GIF plays in real time.
Frames share one palette, built from the first and last frame, so colours stay
steady instead of flickering between frames.
"""
import json
import sys
from pathlib import Path

from PIL import Image

src, out = Path(sys.argv[1]), sys.argv[2]
width = int(sys.argv[3]) if len(sys.argv) > 3 else 0  # 0: as recorded, the sharpest
frames = json.loads((src / "frames.json").read_text())


def load(f):
    im = Image.open(src / f["file"]).convert("RGB")
    if not width or width == im.width:
        return im
    return im.resize((width, round(im.height * width / im.width)), Image.LANCZOS)


first, last = load(frames[0]), load(frames[-1])
sample = Image.new("RGB", (first.width, first.height * 2))
sample.paste(first, (0, 0))
sample.paste(last, (0, first.height))
palette = sample.quantize(colors=255, method=Image.MEDIANCUT)

images, durations = [], []
for i, f in enumerate(frames):
    nxt = frames[i + 1]["t"] if i + 1 < len(frames) else f["t"] + 1500
    images.append(load(f).quantize(palette=palette, dither=Image.NONE))
    durations.append(max(40, nxt - f["t"]))

images[0].save(out, save_all=True, append_images=images[1:], duration=durations, loop=0, optimize=True, disposal=1)
print(out, len(images), "frames,", round(Path(out).stat().st_size / 1e6, 2), "MB")
