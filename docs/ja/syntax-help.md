# Go構文解説カタログ

[ガイド](guide.md) | [English](../en/syntax-help.md)

右ペインは、Go公式の言語仕様を主資料とする小さな教科書です。内容は独自に日本語で要約しています。公式文書の翻訳全文でも、エージェントの生成結果でもありません。説明はバイナリに内蔵しています。閲覧時の通信や外部送信はありません。

## 1項目の構成

1. カーソル上のトークンとソース位置
2. 分類と項目名（例：型 > ポインタ、式 > 参照解除）
3. 意味：何を表し、どう動作するか
4. 書き方：構文の形
5. 使用例：改行・インデント付きのGoコードと、結果や振る舞いのコメント
6. 注意点：nil、範囲外、共有、型の違い、バージョン差など
7. 公式リファレンス：対応する仕様の節のURL

長い説明は `[` / `]` でスクロールします。URLはテキストで表示します。自動では開きません。

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

動作は言語仕様で確認します。[Effective Go](https://go.dev/doc/effective_go) は、慣用的な説明の補助資料です。Effective Goは更新対象ではなく、ジェネリクス等を網羅していません。新しい機能の根拠には言語仕様を優先します。

今回の確認対象は、公開されているGo 1.27仕様です。新しい構文・組み込みに必要なGoのバージョンは、該当項目に明記します。カタログは、対象プロジェクトの `go.mod` に応じて項目を隠すものではありません。

## 構文判定の範囲

- カーソル位置は、元ソースの物理行・rune列を使います。`//line` による仮想位置には置き換えません。
- トークン走査に加え、現在の関数断片のASTを解析します。型の `*T`、参照解除の `*p`、乗算の `a*b` などを区別します。
- `println`・`len` などの組み込み関数や、`string`・`error` などの事前宣言型は、名前から役割を説明します。通常の呼び出し位置以外も対象です。断片内の同名宣言、フィールド名、セレクタは一般の識別子と区別します。断片外の宣言は構文だけでは確定できません。その場合は候補であることと、同名宣言の可能性を注意点に示します。
- 標準ライブラリの38関数には専用の説明があります。対象は `fmt`・`errors`・`strings`・`strconv`・`os`・`io`・`context`・`time`・`log` のよく使う関数です。既存の呼び出し解析が返すパッケージパスと関数名で照合します。importの別名にも対応します。単に `fmt.Println` と書かれた独自メソッドには適用しません。解析で独自関数・関数値と分かる呼び出しでは、同名の組み込み解説も抑制します。
- 標準関数の参考資料は、[Go標準ライブラリ公式リファレンス](https://pkg.go.dev/std) とインストール済みGoの `go doc` です。例えば、組み込み `println` はデバッグ用の実装依存の出力です。[fmt.Println](https://pkg.go.dev/fmt#Println) は標準出力へ書き込み、バイト数とエラーを返します。この違いを区別して説明します。リンクは表示だけで、自動接続しません。
- 式の `f[T]` は、添字か型引数かを構文だけでは判定できません。型の位置など確定できる場合を除き、両方の可能性を説明します。
- 不完全・構文エラーのある断片はAST判定を行いません。トークン単位の説明に戻ります。コメントの中身はコードとして解説しません。

## 更新・検証

解説データは `cmd/hodocode/syntax_help.go` にあります。標準関数の解説は `standard_help.go`、用途判定は `syntax_context.go`、表示は `syntax_explanation.go` に分離しています。

項目の追加時は、7つのデータ欄を揃えます。規則を説明する公式仕様の節か、標準ライブラリのシンボルへの参照も必要です。断定できない意味は推測しません。注意点で限界を説明します。

```sh
go test ./cmd/hodocode -run 'Test(Known|StandardHelp|SourceTopics|SourceExplanations|SyntaxExplanation|SyntaxHelp)'
go test -race ./cmd/hodocode
```

テストは次の内容を検証します。

- 分類の代表例と同名宣言
- Unicode位置と狭いペインの折り返し
- カタログの必須欄
- 全使用例の構文・型チェック
- 一部使用例の実行結果

意味・注意点の文章は、人が公式資料と照合する必要があります。
