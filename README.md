# go-yomikae

日本語 TTS (IrodoriTTS) に渡すテキストから英字を取り除き、カナの読みに置き換える。
Go のライブラリ、CLI、speech.cpp の前に置く OpenAI 互換プロキシとして使える。

```text
IrodoriTTSにGitHubのREADME.mdを読ませたら、v1.2のmp3が5Gで届いた。
イロドリティーティーエスにギットハブのリードミードットエムディーを読ませたら、ヴィー1.2のエムピー3が5ジーで届いた。
```

- 出力には英字が残らない。全角英字、`㎏` や `™` のような互換文字、ギリシャ文字も読みに置き換わる。
- 英字を含まない文は、同じバイト列のまま返る。
- 数字と記号は変えない。
- 英単語は [e2k-go](https://github.com/tenkoh/e2k-go) でカタカナにし、頭字語と一文字は文字ごとに読む。
  読めないものも文字ごとの読みに落とすので、英字が残ることはない。

設計と、その根拠にした調査は [docs/design.md](docs/design.md) にある。

## 用意するもの

C2K の重み (`model-c2k.safetensors`) を一度だけダウンロードする。手順は [docs/models.md](docs/models.md) にある。
重みはライセンスの都合で同梱していない。NGram のデータは同梱している。

## インストール

```bash
go install github.com/tenkoh/go-yomikae/cmd/yomikae@latest
```

## CLI で変換する

標準入力かファイルを一行ずつ変換し、標準出力に書く。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors
```

`-trace` を付けると、置き換えごとに JSON を一行ずつ標準エラーに書く。元のバイト範囲、使った手段
(`dictionary`、`model`、`spell`、`drop`)、辞書のルール、結果が分かる。

```bash
echo "GitHubで10kgのv1.2" | yomikae -c2k model-c2k.safetensors -trace
```

`yomikae stats -check` はファイルの英字スパンの種類と読みの手段を集計し、英字が残った行があれば失敗する。

## プロキシとして動かす

speech.cpp の前に置くと、`POST /v1/audio/speech` の `input` だけを書き換えて転送する。
ほかのメンバー、ヘッダ、ほかのエンドポイントはそのまま通す。SSE と PCM の応答は溜めずに流す。

```bash
yomikae serve -c2k model-c2k.safetensors -upstream http://127.0.0.1:8080 -listen 127.0.0.1:8081
```

クライアントは `http://127.0.0.1:8081/v1/audio/speech` に送る。

## 設定

フラグが環境変数より優先する。`-dict` を一つでも指定すると、`YOMIKAE_DICTS` は使わない。

| フラグ | 環境変数 | 意味 |
| --- | --- | --- |
| `-c2k` | `YOMIKAE_C2K_MODEL` | C2K の重みのパス |
| `-missing-model` | `YOMIKAE_MISSING_MODEL` | 重みがないとき。`error` (既定) で起動を止め、`spell` で警告を出して文字ごとに読む |
| `-ngram` | `YOMIKAE_NGRAM_MODEL` | 同梱の代わりに使う NGram データのパス。`none` で簡易判定 |
| `-dict` | `YOMIKAE_DICTS` | 辞書の TOML ファイル。繰り返し指定でき、後のファイルが優先する。環境変数は `:` 区切り |
| `-no-builtin-dicts` | `YOMIKAE_NO_BUILTIN_DICTS` | 同梱辞書 (IT 用語と単位) を読まない |
| `-licenses` | | 同梱データのライセンス表示を出す |

## 辞書を書く

辞書は TOML で書く。リテラルの項目が既定で、正規表現は `[[regex]]` に書いた項目だけで使う。

```toml
name = "mydict"
case_sensitive = false

[literal]
Irodori = "イロドリ"
".py" = "ドットパイ"

[[regex]]
id = "semver"
pattern = 'v(\d+)\.(\d+)'
replace = "バージョン$1.$2"
```

読みに英字が入っていると、読み込み時にエラーになる。キャプチャに英字が残っていれば、
その部分も変換してから置き換える。一致が重なったときは、最も左、次に最も長い一致を選ぶ。
長さも同じなら、後に指定した辞書が勝つ。同梱辞書は [dict/builtin](dict/builtin) にある。

## ライブラリとして使う

```go
f, _ := os.Open("model-c2k.safetensors")
model, _ := e2k.LoadC2K(f)
dicts, _ := dict.Builtin()
judge, _ := ngram.Builtin()
c, _ := yomikae.New(yomikae.Options{Dictionaries: dicts, Model: model, Judge: judge})
r, _ := c.Convert(ctx, "GitHubで10kg")
fmt.Println(r.Text) // ギットハブで10キログラム
```

`Options.Model` は `ConvertContext(ctx, s) (string, error)` を持つ型なら何でもよい。テストでは偽物に差し替えられる。

## ライセンス

MIT。同梱の NGram データは CMUdict (BSD 2-Clause) に由来する。表示は [ngram/NOTICES.md](ngram/NOTICES.md) にある。
