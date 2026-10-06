"""Generate assets/app.ico and assets/app-unread.ico (requires Pillow)."""
import os
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "assets")
SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]

def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))

def gradient(n):
    stops = [(0.0, (0xF5, 0xA6, 0x23)), (0.55, (0xE8, 0x59, 0x0C)), (1.0, (0xB0, 0x7E, 0xF5))]
    img = Image.new("RGB", (n, n))
    px = img.load()
    for y in range(n):
        for x in range(n):
            t = (x + y) / (2 * (n - 1))
            for i in range(len(stops) - 1):
                t0, c0 = stops[i]; t1, c1 = stops[i + 1]
                if t <= t1:
                    px[x, y] = lerp(c0, c1, (t - t0) / (t1 - t0)); break
    return img

def base(n=1024, badge=False):
    s = n / 48.0
    img = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    mask = Image.new("L", (n, n), 0)
    ImageDraw.Draw(mask).rounded_rectangle([2 * s, 2 * s, 46 * s, 46 * s], radius=11 * s, fill=255)
    img.paste(gradient(n), (0, 0), mask)
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([12.5 * s, 9.5 * s, 35.5 * s, 39 * s], radius=4 * s, fill=(255, 255, 255, 250))
    d.rounded_rectangle([18.5 * s, 6.5 * s, 29.5 * s, 13 * s], radius=2 * s, fill=(44, 45, 42, 255))
    w = int(2.8 * s)
    for cy in (20.5, 30):
        d.line([(16.8 * s, cy * s), (19.4 * s, (cy + 2.5) * s), (24 * s, (cy - 2.4) * s)], fill=(232, 89, 12, 255), width=w, joint="curve")
        d.line([(26.8 * s, (cy + 0.4) * s), (31.6 * s, (cy + 0.4) * s)], fill=(44, 45, 42, 255), width=int(2.5 * s))
    if badge:
        r = 9.5 * s; cx, cy = 39 * s, 9 * s
        d.ellipse([cx - r - 2.2 * s, cy - r - 2.2 * s, cx + r + 2.2 * s, cy + r + 2.2 * s], fill=(255, 255, 255, 255))
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(229, 72, 77, 255))
    return img

for name, badge in (("app.ico", False), ("app-unread.ico", True)):
    big = base(badge=badge)
    imgs = [big.resize((sz, sz), Image.LANCZOS) for sz in SIZES]
    imgs[-1].save(os.path.join(OUT, name), format="ICO", sizes=[(sz, sz) for sz in SIZES], append_images=imgs[:-1])
    if not badge:
        big.resize((256, 256), Image.LANCZOS).save(os.path.join(OUT, "app.png"))
print("ok")
