import sys, json, glob, os
from fontTools.ttLib import TTFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.boundsPen import BoundsPen
out = {}
for fp in sorted(glob.glob(os.path.join(sys.argv[1], "*.ttf"))):
    f = TTFont(fp); gs = f.getGlyphSet(); cmap = f.getBestCmap(); upm = f["head"].unitsPerEm
    asc = f["hhea"].ascent
    x = 0; d = []; ok = True
    for ch in "连镜":
        g = cmap.get(ord(ch))
        if not g: ok = False; break
        pen = SVGPathPen(gs)
        # 翻转 y：字体坐标 y 向上，SVG 向下
        gs[g].draw(TransformPen(pen, (1, 0, 0, -1, x, asc)))
        d.append(pen.getCommands()); x += gs[g].width
    if ok:
        out[os.path.basename(fp)[:-4]] = {"d": d, "w": x, "h": upm, "asc": asc}
    print(os.path.basename(fp), "ok" if ok else "missing", file=sys.stderr)
json.dump(out, open(sys.argv[2], "w"))
