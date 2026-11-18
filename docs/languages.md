# 多言語解析 / Language coverage

`--language auto` が既定です。拡張子から言語を区別し、複数言語を同時に索引化します。`--language go|zig|ts|rust|c|js|python` で解析対象を限定できます。ローカルとGitHubで同じ指定を使えます。

`--language auto` is the default. File extensions identify languages in mixed repositories. Use `--language go|zig|ts|rust|c|js|python` to restrict analysis, locally or on GitHub.

| 言語 / Language | 初期対応 / Initial coverage | 制限 / Limits |
| --- | --- | --- |
| Go | 既存のAST・型情報解析、入れ子のモジュール / existing AST/type-aware analysis, nested modules | `go.mod` とビルド制約に従う / module and build constraints apply |
| Zig | `.zig`: zlsの宣言・定義先 / server symbols and definitions | comptime・間接呼び出しは未確定 / comptime and indirect targets may remain unresolved |
| TypeScript | `.ts`: typescript-language-serverの宣言・定義先 / server symbols and definitions | TypeScript本体・tsconfig・依存の用意が必要 / requires TypeScript and project configuration |
| Rust | `.rs`: rust-analyzerの宣言・定義先 / server symbols and definitions | Cargo設定が必要。build script・proc macroは無効化を要求 / requests disabled build scripts and proc macros |
| C | `.c`, `.h`: clangdの宣言・定義先 / server symbols and definitions | 正確な解析にはcompile_commands.json等が必要 / accurate analysis needs compilation flags |
| JavaScript | `.js`, `.mjs`, `.cjs`: typescript-language-serverの宣言・定義先 / server symbols and definitions | 動的な変更は確定しない / runtime changes remain uncertain |
| Python | `.py`: pyright-langserverの宣言・定義先 / server symbols and definitions | 型情報・環境設定次第。動的属性は未確定 / depends on typing and environment; dynamic attributes remain uncertain |

## 保守的な解析 / Conservative analysis

追加言語は言語サーバーを必須とし、LSPのdocumentSymbolで宣言一覧を構築、definitionで呼び出し位置の定義先を調べます。字句解析だけへのフォールバックはしません。型情報の精度はサーバーとプロジェクト設定に依存し、全言語でGoと同等の精度を保証するものではありません。JS/TSは同じサーバーに渡し、相互の参照を解決できます。

呼び出し位置と本体の境界には既存の字句・構造解析を併用します。そのため、補間・マクロ・生成コード・特殊な構文内の呼び出しを網羅しません。定義先が解析対象内の本体を持つ関数に一意に一致した場合だけ接続します。Zigでは`const name = @import("literal")`を起点とする`.`経由の名前空間呼び出しを、ZLSが解析対象内の関数本体に一意に解決した場合に接続します。同名の引数・捕捉変数や別名への代入がある場合は候補扱いを維持し、一般の`anytype`受け手を確定扱いにしません。JS/TSは`import * as ns from "literal"`による`ns.fn()`、Pythonは`import module [as ns]`で明示したモジュール経路への呼び出し、Rustは`crate`・`self`・`super`・`mod`・`use`経由で解決した自由関数を接続します。JS/TS/Pythonの任意のオブジェクト階層、Rustの関連メソッド・トレイト経由、Cの関数ポインタ、および再代入や隠蔽のある名前は候補・未解決を維持します。Goの型情報に基づく判定は変更しません。複数候補・関数値・範囲外の定義は未解決のまま、理由と既知の候補をTUIに表示します。候補からのジャンプ、仮想ディスパッチの実装型列挙、FFI、JS/TS以外の言語間呼び出しは未対応です。依存一覧の抽出は従来の字句解析によるもので、意味解析とは独立しています。

読み込み時に言語ごとにサーバーを起動し、宣言と呼び出し先をスナップショットに保存して終了します。各言語の解析上限は2分です。Zigの名前空間の定義先が起動直後に空の場合、初回だけZLSの進捗開始を最大2秒待ち、通知されたビルド設定の読み込み完了後に再照会します。進捗通知がないサーバーでは空の結果を推測で補完しません。サーバー未導入、起動失敗、プロトコルエラー、時間切れはエラーとして報告します。

必要な実行ファイルは `zls`、`typescript-language-server --stdio`（TypeScript本体も必要）、`rust-analyzer`、`clangd`、`pyright-langserver --stdio` です。autoでは検出した言語に必要なものだけを起動します。SDK・型スタブ・依存・コンパイル設定は利用者が用意してください。HodoCode自身はツールのインストールや依存取得を行いません。

Language servers are mandatory for non-Go languages. Symbols and definitions come from the server's syntax/type analysis; lexical call extraction remains limited. Only unique indexed function definitions become edges; dynamic members and ambiguous targets remain candidates. Errors do not fall back to lexical-only analysis.

### 外部ツールの信頼 / External-tool trust

言語サーバーはサンドボックスではありません。ソース全文を外部プロセスへ渡します（AIへの送信とは別です）。サーバーはプロジェクト設定・プラグイン・ビルドスクリプトの処理やネットワーク通信を行う可能性があります。Rustではbuild scripts・proc macros・check-on-saveの無効化を要求し、Cではbackground index・clang-tidy・clangd設定を無効化、Zigではbuild-on-saveを無効化しますが、すべてのサーバーの動作を保証するものではありません。GitHubから取得したものを含め、信頼できるリポジトリとツールだけを解析してください。

Language servers receive source text and are not sandboxed. They may execute project hooks or access the network; use only trusted tools and repositories, including GitHub snapshots.

Pythonのf/t文字列とJavaScript/TypeScriptのテンプレート補間の内部呼び出しは抽出しません。字句解析に失敗した場合は読み込みを停止します。

Calls inside Python f/t strings and JavaScript/TypeScript template interpolation are not extracted. Lexical failures stop loading.

Go以外ではGo標準ライブラリ・型・変数定義の解説を転用しません。構文解説は言語ごとの基本項目のみで、未対応のトークンには未対応と表示します。

Non-Go syntax help uses a language-specific basic catalog, never Go standard-library/type/variable explanations. Unsupported tokens are explicitly marked unsupported.

## AI送信 / AI transmission

文字列・コメントは言語別に同じバイト長の空白へ置換します（改行は保持）。未対応言語、字句解析に失敗したソース、解析時点と一致しない追加言語のソースでは送信準備を停止します。安全に区別できない構文は推測でマスクせず停止する場合があります。マスク処理自身は対象コードやPythonモジュールを実行・importしませんが、読み込み時の言語サーバーは上記の信頼条件に従います。

Language-aware masking replaces strings and comments with equal-length spaces while preserving newlines. Unsupported languages, lexical failures, and changed non-Go source snapshots block preparation. Ambiguous constructs may be rejected rather than guessed. Masking itself does not execute target code or import Python modules; language-server loading has the trust requirements above.

これは完全な機密情報除去ではありません。識別子・パス・ユーザーの指示は残り得るため、送信前のプレビューと除外設定を確認してください。

現在、Pythonのf/t文字列、JavaScript/TypeScriptのテンプレート補間、曖昧な正規表現境界などを含むファイルはAI送信準備を停止します。通常の文字列・Rustのraw文字列・Zigの複数行文字列・Cの行継続コメントなどは言語別にマスクします。\n\nCurrently, Python f/t strings, JavaScript/TypeScript template interpolation, and ambiguous regex boundaries block AI preparation for the file. Ordinary strings, Rust raw strings, Zig multiline strings and C line-spliced comments use language-aware masking.\n\nMasking is not complete secret removal: identifiers, paths and user instructions can remain. Audit the preview and exclusions before sending.
