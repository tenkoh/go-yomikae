# Writes the expected scores for tiny.json with the upstream NGram class.
# Usage: uv run --no-project --with numpy python -I gen.py <e2k/inference.py> <dir>
import json
import math
import sys
import zipfile

src, out = sys.argv[1], sys.argv[2]
ns = {"__name__": "upstream"}
exec(open(src).read(), ns)

zpath = f"{out}/tiny.json.zip"
with zipfile.ZipFile(zpath, "w", compression=zipfile.ZIP_LZMA) as z:
    z.write(f"{out}/tiny.json", "ngram.json")
ns["get_asset_path"] = lambda _: zpath
model = ns["NGram"]()

words = [
    "ab", "ABA", "c", "abc", "cc", "bab", "aabbcc",
    "ba ab", "a-b", "x ab", "ab\x1cc", "ab　ba", " ab\t",
    "", "xyz", "  ",
]
cases = []
for w in words:
    s = model.score(w)
    cases.append({
        "word": w,
        "score": None if math.isinf(s) else s,
        "pronounceable": model(w),
    })
with open(f"{out}/tiny-scores.json", "w") as f:
    json.dump(cases, f, indent=1)
