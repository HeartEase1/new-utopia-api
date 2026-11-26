"""Build the coin-only color font from public/utopia-coin.svg.

Optional asset-authoring tool, not part of the application build:
  python -m pip install 'fonttools[woff]'
  python web/scripts/generate-utopia-coin.py

COLR v0 keeps the glyph scalable; the cmap contains only U+1FA99. Browsers
without color-font support use the monochrome coin outline. Copy/paste and
API/CSV output retain the standard coin character, never private-use text.
"""

from pathlib import Path
from xml.etree import ElementTree

from fontTools.colorLib.builder import buildCOLR, buildCPAL
from fontTools.fontBuilder import FontBuilder
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.svgLib.path import parse_path

public = Path(__file__).resolve().parents[1] / "public"
paths = ElementTree.parse(public / "utopia-coin.svg").getroot().findall(
    "{http://www.w3.org/2000/svg}path"
)
names = [".notdef", "coin"] + [f"layer{i}" for i in range(len(paths))]
glyphs = {".notdef": TTGlyphPen(None).glyph()}
palette = []
layers = []
for i, path in enumerate(paths):
    pen = TTGlyphPen(None)
    parse_path(path.attrib["d"], TransformPen(Cu2QuPen(pen, 0.5), (10, 0, 0, -10, 0, 800)))
    glyphs[names[i + 2]] = pen.glyph()
    color = path.attrib["fill"].lstrip("#")
    palette.append(tuple(int(color[j:j + 2], 16) / 255 for j in (0, 2, 4)) + (1.0,))
    layers.append((names[i + 2], i))
glyphs["coin"] = glyphs["layer0"]

font = FontBuilder(1000, isTTF=True)
font.setupGlyphOrder(names)
font.setupCharacterMap({0x1FA99: "coin"})
font.setupGlyf(glyphs)
font.setupHorizontalMetrics({
    name: (1000, getattr(font.font["glyf"][name], "xMin", 0)) for name in names
})
font.setupHorizontalHeader(ascent=850, descent=-150)
font.setupNameTable({
    "familyName": "Utopia Coin", "styleName": "Regular",
    "uniqueFontIdentifier": "UtopiaCoin-Regular-1.0",
    "fullName": "Utopia Coin Regular", "psName": "UtopiaCoin-Regular",
    "version": "Version 1.0",
    "licenseDescription": "GNU Affero General Public License v3.0 or later",
})
font.setupOS2(sTypoAscender=850, sTypoDescender=-150, usWinAscent=850, usWinDescent=150)
font.setupPost()
font.setupMaxp()
font.font["COLR"] = buildCOLR({"coin": layers}, version=0)
font.font["CPAL"] = buildCPAL([palette])
# Stable timestamps make regenerating the same SVG reproducible.
font.font["head"].created = font.font["head"].modified = 3878496000
font.font.recalcTimestamp = False
font.font.flavor = "woff2"
target = public / "fonts" / "utopia-coin.woff2"
target.parent.mkdir(parents=True, exist_ok=True)
font.save(target)
print(f"Generated {target.name}: {target.stat().st_size} bytes")
