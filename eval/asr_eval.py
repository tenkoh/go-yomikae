"""Synthesize marked sentences with speech.cpp, transcribe them, and score how much of the Japanese around the mark survives.

Input is TSV with columns id, condition, text. The text marks the inserted span with {braces}; the score ignores it.
Run: uv run --no-project python -I eval/asr_eval.py IN.tsv OUT_DIR [--seeds 1,2,3]
"""
import argparse
import csv
import json
import re
import subprocess
import sys
import unicodedata
from collections import defaultdict
from pathlib import Path

PUNCT = re.compile(r"[\s、。，．,.!?！？「」『』（）()・:：;；\-ー〜~…]")


def clean(s: str) -> str:
    return PUNCT.sub("", unicodedata.normalize("NFKC", s))


def lcs(a: str, b: str) -> int:
    prev = [0] * (len(b) + 1)
    for ca in a:
        cur = [0]
        for j, cb in enumerate(b):
            cur.append(prev[j] + 1 if ca == cb else max(prev[j + 1], cur[j]))
        prev = cur
    return prev[-1]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("tsv")
    ap.add_argument("out")
    ap.add_argument("--seeds", default="1,2,3")
    ap.add_argument("--tts", default="irodori-tts-mf")
    ap.add_argument("--asr", default="qwen3-asr-0.6b")
    ap.add_argument("--fail-below", type=float, default=0.8)
    a = ap.parse_args()
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    rows = list(csv.reader(open(a.tsv, encoding="utf-8"), delimiter="\t"))
    jobs = []
    for rid, cond, text in rows:
        for seed in a.seeds.split(","):
            wav = out / f"{rid}.{cond}.{seed}.wav"
            if not wav.exists():
                spoken = text.replace("{", "").replace("}", "")
                subprocess.run(["speech", "tts", a.tts, "--voice", "none", "--seed", seed, "-o", str(wav), spoken],
                               check=True, capture_output=True)
            jobs.append((rid, cond, text, seed, wav))
    res = subprocess.run(["speech", "asr", a.asr, "--language", "ja", "--format", "json", *[str(j[4]) for j in jobs]],
                         check=True, capture_output=True, text=True)
    heard = {json.loads(l)["file"]: json.loads(l)["text"] for l in res.stdout.splitlines() if l.strip()}
    by = defaultdict(list)
    with open(out / "results.jsonl", "w", encoding="utf-8") as f:
        for rid, cond, text, seed, wav in jobs:
            context = clean(re.sub(r"\{[^}]*\}", "", text))
            hyp = clean(heard[str(wav)])
            recall = lcs(context, hyp) / len(context)
            by[cond].append(recall)
            f.write(json.dumps({"id": rid, "condition": cond, "seed": seed, "text": text,
                                "asr": heard[str(wav)], "context_recall": round(recall, 3)}, ensure_ascii=False) + "\n")
    print(f"{'condition':14s} {'n':>4s} {'mean':>6s} {'<' + str(a.fail_below):>6s}")
    for cond, rs in by.items():
        bad = sum(r < a.fail_below for r in rs)
        print(f"{cond:14s} {len(rs):4d} {sum(rs) / len(rs):6.3f} {bad / len(rs):6.1%}")


if __name__ == "__main__":
    sys.exit(main())
