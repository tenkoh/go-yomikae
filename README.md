# go-yomikae

go-yomikae は、OpenAI 互換の音声合成 (TTS) サーバーの前に置くリバースプロキシです。
`POST /v1/audio/speech` の `input` から英字を取り除き、カナの読みに置き換えてからサーバーへ転送します。

```text
受け取った input: ClaudeにGitHubのREADME.mdを読ませたら、v1.2のmp3が5Gで届いた。
転送する input:   クロードにギットハブのリードミードットエムディーを読ませたら、ヴィー1.2のエムピー3が5ジーで届いた。
```

同じ変換は、Go のライブラリと `yomikae` コマンドからも使えます。

## 英字をカナにしてから渡す理由

LLM の出力には、製品名、コマンド、頭字語、単位といった英字が多く混ざります。手元の LLM 出力では、7 割の行が英字を含んでいました。
日本語の TTS モデルがこれらをどう読むかは、モデル次第です。IrodoriTTS に読ませて書き起こすと、`café` は「ケフレイ」、ギリシャ文字の `α` は「あー」になりました。
モデルが外した読みは、利用者の側からは直せません。

go-yomikae は、英字がモデルに届く前にカナへ置き換えます。読みを決めるのはモデルではなく go-yomikae なので、外れた読みは辞書で直せます。
どの英字をどの手段で読んだかは、追跡情報で確かめられます。

go-yomikae は日本語の TTS を使う技術者向けで、主に IrodoriTTS と speech.cpp の組み合わせで確かめています。
調査と測定の結果は [docs/design.md](docs/design.md) にあります。

## 変換の決まり

- 出力に英字は残りません。全角英字、`㎏` や `™` のような互換文字、ギリシャ文字も読みに置き換えます。
- 英字を含まない文は、同じバイト列のまま返します。
- 数字と記号は変えません。
- 辞書の項目を最優先します。同梱辞書には IT 用語と単位が入っています。
- 辞書にない英単語は [e2k-go](https://github.com/tenkoh/e2k-go) でカタカナにします。頭字語と一文字は、文字ごとに読みます。
- e2k-go が読めない語も文字ごとの読みに落とすので、英字は残りません。

## プロキシを動かす

1. C2K の重み (`model-c2k.safetensors`) をダウンロードします。手順は [docs/models.md](docs/models.md) にあります。
   重みはライセンスの都合で同梱していません。

2. `yomikae` をインストールします。

   ```bash
   go install github.com/tenkoh/go-yomikae/cmd/yomikae@latest
   ```

3. TTS サーバーを `-upstream` に指定して、プロキシを起動します。

   ```bash
   yomikae serve -c2k model-c2k.safetensors -upstream http://127.0.0.1:8080
   ```

   プロキシは `127.0.0.1:8081` で待ち受けます。変えるときは `-listen` を指定します。

4. クライアントの送り先を、TTS サーバーからプロキシに変えます。OpenAI の SDK なら、base URL を `http://127.0.0.1:8081/v1` にします。

## プロキシが書き換える範囲

プロキシが書き換えるのは、`POST /v1/audio/speech` の本文のうち、いちばん外側の JSON オブジェクトにある `input` の文字列だけです。
パス、メソッド、ヘッダ、ほかのメンバーは変えません。ほかのエンドポイントへのリクエストは、そのまま転送します。
応答は溜めずに流すので、SSE や PCM のストリームもそのまま届きます。

対象のパスは、今は `/v1/audio/speech` に限っています。OpenAI 互換の API では `/v1/embeddings` や `/v1/responses` も `input` を持つので、全パスを書き換えると TTS 以外の入力まで変わってしまうからです。
パスの前に何か付くサーバー (`/api/v1/audio/speech` など) では、何も変換せずに転送します。

上限、エラーの応答、細かい条件は [docs/reference.md](docs/reference.md#プロキシ) にあります。

## コマンドで変換する

`yomikae` に文を渡すと、変換した文を標準出力に書きます。プロキシに入れる前に、読みを確かめるのに使えます。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors
```

`-trace` を付けると、置き換えごとの記録を JSON で標準エラーに書きます。範囲、手段 (`dictionary`、`model`、`spell`、`drop`)、当たった辞書の項目が分かります。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors -trace
```

`yomikae stats -check` は、ファイルの英字スパンの種類と読みの手段を集計します。英字が残った行があれば失敗します。

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

辞書は TOML で書き、`-dict` で指定します。後に指定した辞書ほど優先します。

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

読みに英字が入っていると、読み込み時にエラーになります。辞書の形式、一致の優先順、すべてのフラグと環境変数は [docs/reference.md](docs/reference.md) にあります。

## ライセンス

MIT です。同梱の NGram データは CMUdict (BSD 2-Clause) に由来します。表示は [ngram/NOTICES.md](ngram/NOTICES.md) にあります。
