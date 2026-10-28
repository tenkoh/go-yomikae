# go-yomikae

go-yomikae は、OpenAI 互換の音声合成 (TTS) サーバーの前に置くリバースプロキシだ。
`POST /v1/audio/speech` の `input` から英字を取り除き、カナの読みに置き換えてからサーバーへ転送する。

```text
受け取った input: ClaudeにGitHubのREADME.mdを読ませたら、v1.2のmp3が5Gで届いた。
転送する input:   クロードにギットハブのリードミードットエムディーを読ませたら、ヴィー1.2のエムピー3が5ジーで届いた。
```

同じ変換は、Go のライブラリと `yomikae` コマンドからも使える。

## 英字をカナにしてから渡す理由

LLM の出力には、製品名、コマンド、頭字語、単位といった英字が多く混ざる。手元の LLM 出力では、7 割の行が英字を含んでいた。
日本語の TTS モデルがこれらをどう読むかは、モデル次第だ。IrodoriTTS に読ませて書き起こすと、`café` は「ケフレイ」、ギリシャ文字の `α` は「あー」になった。
モデルが外した読みは、利用者の側からは直せない。

go-yomikae は、英字がモデルに届く前にカナへ置き換える。読みを決めるのはモデルではなく go-yomikae なので、外れた読みは辞書で直せる。
どの英字をどの手段で読んだかは、追跡情報で確かめられる。

go-yomikae は日本語の TTS を使う技術者向けで、主に IrodoriTTS と speech.cpp の組み合わせで確かめている。
調査と測定の結果は [docs/design.md](docs/design.md) にある。

## 変換の決まり

- 出力に英字は残らない。全角英字、`㎏` や `™` のような互換文字、ギリシャ文字も読みに置き換える。
- 英字を含まない文は、同じバイト列のまま返す。
- 数字と記号は変えない。
- 辞書の項目を最優先する。同梱辞書は IT 用語と単位を収める。
- 辞書にない英単語は [e2k-go](https://github.com/tenkoh/e2k-go) でカタカナにする。頭字語と一文字は、文字ごとに読む。
- e2k-go が読めない語も文字ごとの読みに落とすので、英字は残らない。

## プロキシを動かす

1. C2K の重み (`model-c2k.safetensors`) をダウンロードする。手順は [docs/models.md](docs/models.md) にある。
   重みはライセンスの都合で同梱していない。

2. `yomikae` をインストールする。

   ```bash
   go install github.com/tenkoh/go-yomikae/cmd/yomikae@latest
   ```

3. TTS サーバーを `-upstream` に指定して、プロキシを起動する。

   ```bash
   yomikae serve -c2k model-c2k.safetensors -upstream http://127.0.0.1:8080
   ```

   プロキシは `127.0.0.1:8081` で待ち受ける。変えるときは `-listen` を指定する。

4. クライアントの送り先を、TTS サーバーからプロキシに変える。OpenAI の SDK なら、base URL を `http://127.0.0.1:8081/v1` にする。

## プロキシが書き換える範囲

プロキシが書き換えるのは、`POST /v1/audio/speech` の本文のうち、いちばん外側の JSON オブジェクトにある `input` の文字列だけだ。
パス、メソッド、ヘッダ、ほかのメンバーは変えない。ほかのエンドポイントへのリクエストは、そのまま転送する。
応答は溜めずに流すので、SSE や PCM のストリームもそのまま届く。

対象のパスは、今は `/v1/audio/speech` に限っている。OpenAI 互換の API では `/v1/embeddings` や `/v1/responses` も `input` を持つので、全パスを書き換えると TTS 以外の入力まで変わってしまう。
パスの前に何か付くサーバー (`/api/v1/audio/speech` など) では、何も変換されずに転送される。

上限、エラーの応答、細かい条件は [docs/reference.md](docs/reference.md#プロキシ) にある。

## コマンドで変換する

`yomikae` に文を渡すと、変換した文を標準出力に書く。プロキシに入れる前に、読みを確かめるのに使える。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors
```

`-trace` を付けると、置き換えごとの記録を JSON で標準エラーに書く。範囲、手段 (`dictionary`、`model`、`spell`、`drop`)、当たった辞書の項目が分かる。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors -trace
```

`yomikae stats -check` はファイルの英字スパンの種類と読みの手段を集計し、英字が残った行があれば失敗する。

## Go から使う

```go
f, _ := os.Open("model-c2k.safetensors")
model, _ := e2k.LoadC2K(f)
dicts, _ := dict.Builtin()
judge, _ := ngram.Builtin()
c, _ := yomikae.New(yomikae.Options{Dictionaries: dicts, Model: model, Judge: judge})
r, _ := c.Convert(ctx, "GitHubで10kg")
fmt.Println(r.Text) // ギットハブで10キログラム
```

## 読みを辞書で直す

辞書は TOML で書き、`-dict` で指定する。後に指定した辞書ほど優先する。

```toml
name = "mydict"

[literal]
Irodori = "イロドリ"

[[regex]]
id = "semver"
pattern = 'v(\d+)\.(\d+)'
replace = "バージョン$1.$2"
```

```bash
yomikae serve -c2k model-c2k.safetensors -upstream http://127.0.0.1:8080 -dict mydict.toml
```

読みに英字が入っていると、読み込み時にエラーになる。辞書の形式、一致の優先順、すべてのフラグと環境変数は [docs/reference.md](docs/reference.md) にある。

## ライセンス

MIT。同梱の NGram データは CMUdict (BSD 2-Clause) に由来する。表示は [ngram/NOTICES.md](ngram/NOTICES.md) にある。
