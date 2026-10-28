# Bundled NGram model

`ngram.json.gz` is `ngram.json` from the upstream release, recompressed with gzip.
The license terms are in [NOTICES.md](../NOTICES.md).

| File | SHA-256 |
| --- | --- |
| `ngram.json.zip` (upstream release asset) | `475e573aa9ffb30e2b27a1cabfb907ca7ff3ea19b75cdc6e0d501bc4326742a2` |
| `ngram.json` (its single member) | `356be761aba65e89449fe97ef3b4a3324921a8367f7365e672298e5dea720ee2` |

To regenerate the file:

```bash
curl -fL -o ngram.json.zip https://github.com/Patchethium/e2k/releases/download/v0.8.1/ngram.json.zip
bsdtar -xf ngram.json.zip
gzip -9 -n -c ngram.json > ngram.json.gz
```

`TestBuiltin` checks that the bundled file decompresses to the `ngram.json` above.
