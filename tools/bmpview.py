#!/usr/bin/env python3
"""Render a horizontal strip of a BMP as ASCII, to inspect what is actually drawn.

Usage: bmpview.py file.bmp y0 y1 [cols] [rows]
"""
import struct
import sys


def load_bmp(path):
    d = open(path, 'rb').read()
    if d[:2] != b'BM':
        raise SystemExit('not a BMP: %s' % path)
    pixoff = struct.unpack('<I', d[10:14])[0]
    w = struct.unpack('<i', d[18:22])[0]
    h = struct.unpack('<i', d[22:26])[0]
    bpp = struct.unpack('<H', d[28:30])[0]
    if bpp not in (24, 32):
        raise SystemExit('unsupported bpp=%d' % bpp)
    bytespp = bpp // 8
    rowsize = ((bpp * w + 31) // 32) * 4
    topdown = h < 0
    H = abs(h)

    def px(x, y):
        yy = y if topdown else H - 1 - y
        o = pixoff + yy * rowsize + x * bytespp
        return d[o + 2], d[o + 1], d[o]  # r, g, b

    return w, H, px


# dark -> light ramp
RAMP = ' .:-=+*#%@'


def main():
    path, y0, y1 = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
    cols = int(sys.argv[4]) if len(sys.argv) > 4 else 120
    rows = int(sys.argv[5]) if len(sys.argv) > 5 else 18
    w, h, px = load_bmp(path)
    y1 = min(y1, h)
    print('%s  %dx%d  strip y=%d..%d  (%d px tall)' % (path, w, h, y0, y1, y1 - y0))
    bw = max(1, w // cols)
    bh = max(1, (y1 - y0) // rows)
    for r in range(rows):
        yy = y0 + r * bh
        line = []
        for c in range(cols):
            xx = c * bw
            tot = n = 0
            for sy in range(yy, min(yy + bh, y1)):
                for sx in range(xx, min(xx + bw, w)):
                    rr, gg, bb = px(sx, sy)
                    tot += (rr * 299 + gg * 587 + bb * 114) // 1000
                    n += 1
            lum = tot // n if n else 0
            line.append(RAMP[lum * (len(RAMP) - 1) // 255])
        print('%4d |%s|' % (yy, ''.join(line)))


if __name__ == '__main__':
    main()
