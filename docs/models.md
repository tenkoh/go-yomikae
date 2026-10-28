# モデルデータを用意する

go-yomikae は e2k の C2K の重みを同梱しない。Patchethium/e2k のリリースから、サーバーへ一度だけダウンロードする。
NGram のデータは同梱している。go-yomikae は実行中に外部と通信しない。

## C2K の重み (必須)

英単語をカタカナにするモデルの重みだ。e2k-go の README と同じファイルを使う。

1. 重みをダウンロードする。

   ```bash
   curl -fL -o model-c2k.safetensors https://github.com/Patchethium/e2k/releases/download/0.4.0/model-c2k.safetensors
   ```

2. SHA-256 を確かめる。

   ```bash
   shasum -a 256 model-c2k.safetensors
   ```

   結果が `8dc015ba1574b386cacf54d498e9d418e3c03fb7da58a65b88bc81e454705ca1` であること。

3. パスを `-c2k` か `YOMIKAE_C2K_MODEL` で指定する。

重みは CC-BY-SA の辞書から派生した可能性がある。再配布する前に本家の条件を確かめること。

## NGram のデータ (同梱)

英単語として読むか一文字ずつ読むかを決めるデータで、本家 v0.8.1 の `ngram.json` を同梱している。
用意する必要はない。ライセンスは [ngram/NOTICES.md](../ngram/NOTICES.md) にあり、`yomikae -licenses` でも表示できる。

別のデータを使うときは、展開した `ngram.json` のパスを `-ngram` か `YOMIKAE_NGRAM_MODEL` で指定する。
`-ngram=none` を指定すると、母音と子音の並びで決める簡易判定を使う。
