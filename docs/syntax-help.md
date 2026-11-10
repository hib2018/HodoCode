# Go構文解説カタログ

右ペインは、Go公式の言語仕様を主資料として独自に日本語で要約した、小さな教科書です。公式文書の翻訳全文やエージェントの生成結果ではありません。説明はバイナリに内蔵し、閲覧時の通信・外部送信はありません。

## 1項目の構成

1. カーソル上のトークンとソース位置
2. 分類と項目名（例：型 > ポインタ、式 > 参照解除）
3. 意味：何を表し、どう動作するか
4. 書き方：構文の形
5. 使用例：改行・インデント付きのGoコードと、結果や振る舞いのコメント
6. 注意点：nil、範囲外、共有、型の違い、バージョン差など
7. 公式リファレンス：対応する仕様の節のURL

長い説明は `[` / `]` でスクロールします。URLもテキストとして表示し、自動では開きません。

## 分類と主な参考資料

| 分野 | 細分化の例 | 公式資料 |
| --- | --- | --- |
| 宣言・名前 | 変数、定数、短い変数宣言、型定義と別名、スコープ | [Declarations and scope](https://go.dev/ref/spec#Declarations_and_scope) |
| 関数 | 名前付き関数、メソッド、関数リテラル、関数型、可変長引数 | [Function declarations](https://go.dev/ref/spec#Function_declarations)、[Function types](https://go.dev/ref/spec#Function_types) |
| 制御 | 条件分岐、式switchと型switch、3形式のfor、range | [Statements](https://go.dev/ref/spec#Statements)、[For statements](https://go.dev/ref/spec#For_statements) |
| 型・複合値 | ポインタ、配列、スライス、map、struct、interface、型引数 | [Types](https://go.dev/ref/spec#Types)、[Instantiations](https://go.dev/ref/spec#Instantiations) |
| 式 | 算術・論理・ビット演算、アドレス取得、参照解除、添字、スライス、型アサーション | [Expressions](https://go.dev/ref/spec#Expressions) |
| 並行処理 | go、defer、チャネル型の方向、送信・受信、select | [Channel types](https://go.dev/ref/spec#Channel_types)、[Select statements](https://go.dev/ref/spec#Select_statements) |
| 組み込み | len/cap、make/new、append/copy、delete/clear、close、panic/recover等 | [Built-in functions](https://go.dev/ref/spec#Built-in_functions) |
| 字句 | 数値・rune、解釈済み文字列とraw文字列、区切り記号 | [Lexical elements](https://go.dev/ref/spec#Lexical_elements) |

仕様で動作を確認し、[Effective Go](https://go.dev/doc/effective_go) は慣用的な説明の補助資料とします。Effective Goは更新対象ではなくジェネリクス等を網羅しないため、新しい機能の根拠には言語仕様を優先します。今回の確認対象は公開されているGo 1.27仕様です。新しい構文・組み込みに必要なGoのバージョンは該当項目に明記します。カタログは対象プロジェクトの `go.mod` に応じて項目を隠すものではありません。

## 構文判定の範囲

- カーソル位置は元ソースの物理行・rune列を使います。`//line` による仮想位置には置き換えません。
- トークン走査に加えて現在の関数断片のASTを解析し、型の `*T`、参照解除の `*p`、乗算の `a*b` などを区別します。
- 関数断片内で確認できる同名宣言は組み込み名の説明と区別します。ただしパッケージ全体の名前・型・実行時の値は解決しません。`len` や `int` などは事前宣言名の候補として説明し、同名宣言があり得ることを注意書きに示します。
- 式の `f[T]` は添字か型引数かを構文だけでは判定できません。型の位置など確定できる場合を除き、両方の可能性を説明します。
- 不完全・構文エラーのある断片はAST判定を行わず、トークン単位の説明に戻ります。コメントの中身はコードとして解説しません。

## 更新・検証

解説データは `cmd/hodocode/syntax_help.go`、用途判定は `syntax_context.go`、表示は `syntax_explanation.go` に分離しています。項目を追加するときは7つのデータ欄と、その規則を説明する公式仕様の節を揃えます。断定できない意味は推測せず、注意点で限界を説明します。

```sh
go test ./cmd/hodocode -run 'Test(SourceTopics|SourceExplanations|SyntaxExplanation|SyntaxHelp)'
go test -race ./cmd/hodocode
```

テストは分類の代表例、同名宣言、Unicode位置、狭いペインの折り返し、カタログの必須欄、全使用例の構文・型チェック、一部使用例の実行結果を検証します。意味・注意点の文章自体の正しさは公式資料との人による照合が必要です。
