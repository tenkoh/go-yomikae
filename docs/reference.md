# リファレンス

`yomikae` コマンド、プロキシ、辞書の形式、追跡情報の形式をまとめます。使い方の流れは [README](../README.md) にあります。

## コマンド

```text
yomikae [convert] [flags] [file ...]
yomikae stats [flags] file ...
yomikae serve [flags]
```

サブコマンドを省くと `convert` になります。各サブコマンドに `-h` を付けると、フラグの一覧を表示します。

### convert

ファイルを一行ずつ変換し、標準出力に書きます。ファイルを指定しないときは標準入力を読みます。

| フラグ | 既定 | 意味 |
| --- | --- | --- |
| `-trace` | `false` | 置き換えごとに JSON を一行ずつ標準エラーに書きます。形式は「[追跡情報](#追跡情報)」にあります |

### stats

ファイルの英字スパンの種類と、読みの手段 (`dictionary`、`model`、`spell`、`drop`) を集計します。

| フラグ | 既定 | 意味 |
| --- | --- | --- |
| `-check` | `false` | 出力に英字が残った行を表示し、0 以外の終了コードで終わります |
| `-top` | `5` | 行ごとに表示する例の数 |

### serve

OpenAI 互換の TTS プロキシを起動します。動きは「[プロキシ](#プロキシ)」にあります。

| フラグ | 既定 | 意味 |
| --- | --- | --- |
| `-listen` | `127.0.0.1:8081` | 待ち受けるアドレス |
| `-upstream` | なし (必須) | 転送先の TTS サーバーの URL。例は `http://127.0.0.1:8080` です |
| `-max-body` | `1048576` | 書き換える本文の上限 (バイト) |

`serve` のフラグには環境変数がありません。

### 共通のフラグ

どのサブコマンドでも使えます。フラグを指定すると、対応する環境変数より優先します。
`-dict` を一つでも指定すると、`YOMIKAE_DICTS` は読みません。

| フラグ | 環境変数 | 既定 | 意味 |
| --- | --- | --- | --- |
| `-c2k` | `YOMIKAE_C2K_MODEL` | なし | C2K の重みのパス。用意の手順は [docs/models.md](models.md) にあります |
| `-missing-model` | `YOMIKAE_MISSING_MODEL` | `error` | 重みがないときの動き。`error` は起動を止めます。`spell` は警告を出し、英単語を文字ごとに読みます |
| `-ngram` | `YOMIKAE_NGRAM_MODEL` | 同梱のデータ | 同梱の代わりに使う NGram データのパス。`none` にすると母音と子音の並びで判定します |
| `-dict` | `YOMIKAE_DICTS` | なし | 辞書の TOML ファイル。繰り返し指定できます。環境変数では `:` で区切ります |
| `-no-builtin-dicts` | `YOMIKAE_NO_BUILTIN_DICTS` | `false` | 同梱辞書 (IT 用語と単位) を読みません |
| `-licenses` | なし | `false` | 同梱データのライセンス表示を出して終わります |

## プロキシ

`yomikae serve` は、受け取ったリクエストを `-upstream` のサーバーへ転送します。書き換えるのは、次の条件をすべて満たす部分だけです。

- メソッドが `POST` である。
- パスが `/v1/audio/speech` と完全に一致する。
- 本文がいちばん外側で一つの JSON オブジェクトになっている。
- メンバーの名前が `input` で、値が文字列である。

書き換えた後は、本文の長さに合わせて `Content-Length` を付け直します。パス、メソッド、クエリ、ほかのヘッダ、ほかのメンバーは変えません。
メソッドかパスが合わないリクエストは、本文を読まずにそのまま転送します。応答は溜めずに流すので、SSE と PCM のストリームもそのまま届きます。

| 状況 | 応答 |
| --- | --- |
| 書き換える本文が `-max-body` を超えた | `413 Request Entity Too Large` |
| 本文の読み込みに失敗した | `400 Bad Request` |
| 変換中にクライアントが切断した | `502 Bad Gateway` |
| 本文が JSON オブジェクトでない | 書き換えずに転送します |

上流のエラーは、そのままクライアントに返します。

## 辞書

辞書は TOML ファイルで書きます。

```toml
name = "mydict"
license = "MIT"
case_sensitive = false

[literal]
Irodori = "イロドリ"
".py" = "ドットパイ"

[[regex]]
id = "semver"
pattern = 'v(\d+)\.(\d+)'
replace = "バージョン$1.$2"
```

| キー | 既定 | 意味 |
| --- | --- | --- |
| `name` | ファイル名から拡張子を除いたもの | 追跡情報の `rule` に入る辞書の名前 |
| `license` | なし | 辞書のライセンス。動作には使いません |
| `case_sensitive` | `false` | `true` にすると、キーと正規表現が大文字小文字を区別します |
| `[literal]` | なし | キーと読みの組。キーは文字列のまま照合します |
| `[[regex]]` | なし | 正規表現の項目。`id`、`pattern` (RE2)、`replace` を持ちます |

照合は、全角英字や互換文字を NFKC で揃えてから行います。`ＧｉｔＨｕｂ` も `GitHub` のキーに当たります。
英字で始まるキーは、直前が英字のときには当たりません。英字で終わるキーは、直後が英字のときには当たりません。
このため `Go` のキーは `Google` の中には当たりません。数字との隣接は許すので、`Go1.18` の `Go` には当たります。
ドットなどの記号で始まるキーは、英字の直後でも当たります。`.py` のキーは `main.py` の `.py` に当たります。

一致が重なったときは、次の順に選びます。

1. いちばん左から始まる一致。
2. いちばん長い一致。
3. 後に指定した辞書の一致。同梱辞書は最初に読みます。
4. 同じ辞書の中では、リテラルの項目。次に、先に書いた正規表現の項目。

`replace` のキャプチャ (`$1`) に英字が残ったときは、その部分も変換してから置き換えます。

次の場合は、読み込み時にエラーになります。

- 読み (`literal` の値、または `replace` のキャプチャ以外の部分) が英字を含む。
- `literal` のキーが英字を含まない。
- `case_sensitive = false` の辞書で、大文字小文字を揃えると同じになるキーが二つある。
- `pattern` が RE2 として不正、または `replace` が存在しないキャプチャを参照する。
- `regex` の `id` が欠けている、または重複している。

同梱辞書は [dict/builtin](../dict/builtin) にあります。

## 追跡情報

`yomikae convert -trace` は、置き換えごとに次の JSON を一行ずつ標準エラーに書きます。

| フィールド | 意味 |
| --- | --- |
| `file` | 入力ファイルのパス。標準入力のときは省きます |
| `line` | 行番号。1 から数えます |
| `start`、`end` | 行の中で置き換えた範囲のバイト位置 |
| `source` | 置き換える前の文字列 |
| `result` | 置き換えた後の文字列 |
| `method` | 読みの手段。`dictionary`、`model`、`spell`、`drop` のどれか |
| `class` | 部品の種類。`word`、`acronym`、`letter` のどれか。辞書の一致では省きます |
| `rule` | 当たった辞書の項目。`tech:GitHub` のように、辞書の名前とキー、または `id` を書きます |
| `capture` | 正規表現のキャプチャを変換した記録のときに `true` |

`method` の値の意味は次のとおりです。

| 値 | 意味 |
| --- | --- |
| `dictionary` | 辞書の項目で置き換えた |
| `model` | C2K のモデルでカタカナにした |
| `spell` | 文字ごとに読んだ |
| `drop` | 読みがないので英字を消した |

## ライブラリ

`yomikae.New` で `Converter` を作り、`Convert` で変換します。

```go
f, _ := os.Open("model-c2k.safetensors")
model, _ := e2k.LoadC2K(f)
dicts, _ := dict.Builtin()
judge, _ := ngram.Builtin()
c, _ := yomikae.New(yomikae.Options{Dictionaries: dicts, Model: model, Judge: judge})
r, _ := c.Convert(ctx, "GitHubで10kg")
fmt.Println(r.Text) // ギットハブで10キログラム
```

`Options.Model` には、`ConvertContext(ctx, s) (string, error)` を持つ型なら何でも渡せます。テストでは偽物に差し替えられます。
`Result.Rewrites` は、追跡情報と同じ内容を Go の値で持ちます。

`proxy.New` は、プロキシの `http.Handler` を返します。`yomikae serve` はこれを使っています。
