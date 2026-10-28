# go-yomikae

go-yomikae は、OpenAI 互換の音声合成 (TTS) サーバーの前に置くリバースプロキシです。
`POST /v1/audio/speech` の `input` から英字を取り除き、カナの読みに置き換えてからサーバーへ転送します。

```text
受け取った input: ClaudeにGitHubのREADME.mdを読ませたら、v1.2のmp3が5Gで届いた。
転送する input:   クロードにギットハブのリードミードットエムディーを読ませたら、ヴィー1.2のエムピー3が5ジーで届いた。
```

同じ変換は、Go のライブラリと `yomikae` コマンドからも使えます。

## 英字をカナにしてから渡す理由

日本語の TTS モデルは、英字が混ざった文をうまく読み上げられないことがあります。どこまで読めるかはモデル次第です。１つの解決策は、事前に英字をカタカナなどの"読み"に変換してしまうことです。

go-yomikae は、英字がモデルに届く前にカナへ置き換えます。

## 変換の決まり

- 出力に英字は残りません。全角英字、`㎏` や `™` のような互換文字、ギリシャ文字も読みに置き換えます。
- 英字を含まない文は、同じバイト列のまま返します。
- 数字と記号は変えません。
- 事前定義済みの辞書の項目を最優先します。同梱辞書には IT 用語と単位が入っています。
- 辞書にない英単語は [e2k-go](https://github.com/tenkoh/e2k-go) でカタカナにします。頭字語と一文字は、文字ごとに読みます。
- e2k-go が読めない語も文字ごとの読みに落とすので、英字は残りません。

## プロキシを動かす

### 1. 重みをダウンロードする

英単語をカタカナにするモデルの重み (`model-c2k.safetensors`) をダウンロードします。
重みはライセンスの都合で同梱していないので、[Patchethium/e2k](https://github.com/Patchethium/e2k) のリリースから一度だけ取得します。

```bash
curl -fL -o model-c2k.safetensors https://github.com/Patchethium/e2k/releases/download/0.4.0/model-c2k.safetensors
```

詳細な手順と重みのライセンスは [docs/models.md](docs/models.md) に表記してあります。

### 2. yomikae をインストールする

```bash
go install github.com/tenkoh/go-yomikae/cmd/yomikae@latest
```

### 3. プロキシを起動する

TTS サーバーを `-upstream` に指定して、プロキシを起動します。

```bash
yomikae serve -c2k model-c2k.safetensors -upstream http://127.0.0.1:8080
```

プロキシは `127.0.0.1:8081` で待ち受けます。変えるときは `-listen` を指定します。

### 4. クライアントの送り先を変える

クライアントの送り先を、TTS サーバーからプロキシに変えます。OpenAI の SDK なら、base URL を `http://127.0.0.1:8081/v1` にします。

## プロキシが書き換える範囲

プロキシが書き換えるのは、`POST /v1/audio/speech` の本文のうち、いちばん外側の JSON オブジェクトにある `input` の文字列だけです。
パス、メソッド、ヘッダ、ほかのメンバーは変えません。ほかのエンドポイントへのリクエストは、そのまま転送します。
応答は溜めずに流すので、SSE や PCM のストリームもそのまま届きます。

対象のパスは、今は `/v1/audio/speech` に限っています。

上限、エラーの応答、細かい条件は [docs/reference.md](docs/reference.md#プロキシ) にあります。

## コマンドで変換する

`yomikae` に文を渡すと、変換した文を標準出力に書きます。プロキシに入れる前に、読みを確かめるのに使えます。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors
```

`-trace` を付けると、置き換えごとの記録を JSON で標準エラーに書きます。範囲、手段 (`dictionary`、`model`、`spell`、`drop`)、使用された辞書の項目が分かります。

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
yomikae = "ヨミカエ"

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

MIT です。同梱の NGram データは CMUdict (BSD 2-Clause) に由来します。ライセンス表示は [ngram/NOTICES.md](ngram/NOTICES.md) にあります。
