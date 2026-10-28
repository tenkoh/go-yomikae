"""Build the input of asr_eval.py from corpus lines: each sampled line as raw, as converted by yomikae, and Latin-free control lines.

Run: uv run --no-project python -I eval/make_pairs.py OUT.tsv YOMIKAE_BIN C2K_WEIGHTS CORPUS... [--with-letters 100] [--controls 40]
Lines from .md files skip the front matter and fenced code. Only lines with Japanese and 15 to 120 characters are used,
since IrodoriTTS speaks a sentence at most 30 s at a time.
"""
import argparse
import json
import random
import re
import subprocess

JA = re.compile(r"[ぁ-んァ-ヶ一-龯]")
LETTER = re.compile(r"[A-Za-zＡ-Ｚａ-ｚÀ-ɏͰ-Ͽ]")


def lines(path):
    t = open(path, encoding="utf-8").read()
    if path.endswith(".md"):
        t = re.sub(r"\A---.*?---", "", t, flags=re.S)
        t = re.sub(r"```.*?```", "\n", t, flags=re.S)
        t = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", t)
        t = re.sub(r"^[#>*\-\s|]+", "", t, flags=re.M)
    for line in t.splitlines():
        line = line.strip().replace("{", "").replace("}", "").replace("\t", " ")
        if JA.search(line) and 15 <= len(line) <= 120:
            yield line


def mark(text, spans):
    out, prev = [], 0
    for s, e, rep in spans:
        out.append(text[prev:s] + "{" + rep + "}")
        prev = e
    return "".join(out) + text[prev:]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("bin")
    ap.add_argument("c2k")
    ap.add_argument("corpus", nargs="+")
    ap.add_argument("--with-letters", type=int, default=100)
    ap.add_argument("--controls", type=int, default=40)
    ap.add_argument("--seed", type=int, default=1)
    a = ap.parse_args()
    pool = sorted({l for p in a.corpus for l in lines(p)})
    rng = random.Random(a.seed)
    lettered = rng.sample([l for l in pool if LETTER.search(l)], a.with_letters)
    controls = rng.sample([l for l in pool if not LETTER.search(l)], a.controls)
    res = subprocess.run([a.bin, "-c2k", a.c2k, "-trace"], input="\n".join(lettered) + "\n",
                         capture_output=True, text=True, check=True)
    converted = res.stdout.splitlines()
    rewrites = [[] for _ in lettered]
    for rec in map(json.loads, res.stderr.splitlines()):
        if not rec.get("capture"):
            rewrites[rec["line"] - 1].append(rec)
    with open(a.out, "w", encoding="utf-8") as f:
        for i, (line, rws) in enumerate(zip(lettered, rewrites)):
            b = line.encode()
            raw = []
            for r in rws:
                s = len(b[:r["start"]].decode())
                e = len(b[:r["end"]].decode())
                raw.append((s, e, line[s:e]))
            f.write(f"l{i}\traw\t{mark(line, raw)}\n")
            f.write(f"l{i}\tprocessed\t{mark_converted(line, raw, rws, converted[i])}\n")
        for i, line in enumerate(controls):
            f.write(f"k{i}\tcontrol\t{line}\n")


def mark_converted(line, raw, rws, conv_text):
    out, prev = [], 0
    for (s, e, _), r in zip(raw, rws):
        out.append(line[prev:s] + "{" + r["result"] + "}")
        prev = e
    marked = "".join(out) + line[prev:]
    if marked.replace("{", "").replace("}", "") != conv_text:
        raise SystemExit(f"trace does not reproduce the output: {conv_text}")
    return marked


if __name__ == "__main__":
    main()
