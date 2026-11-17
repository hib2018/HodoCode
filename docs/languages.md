# 多言語解析 / Language coverage

`--language auto` が既定です。拡張子から言語を区別し、複数言語を同時に索引化します。`--language go|zig|ts|rust|c|js|python` で解析対象を限定できます。ローカルとGitHubで同じ指定を使えます。

`--language auto` is the default. File extensions identify languages in mixed repositories. Use `--language go|zig|ts|rust|c|js|python` to restrict analysis, locally or on GitHub.

| 言語 / Language | 初期対応 / Initial coverage | 制限 / Limits |
| --- | --- | --- |
| Go | 既存のAST・型情報解析、入れ子のモジュール / existing AST/type-aware analysis, nested modules | `go.mod` とビルド制約に従う / module and build constraints apply |
| Zig | `.zig`: fn, `@import`, direct calls | comptime・間接呼び出しは未解決 / comptime and indirect targets remain unresolved |
| TypeScript | `.ts`: functions, arrows, class methods, import/export | 型・tsconfig・パスエイリアスを解決しない / no type, tsconfig, or path-alias resolution |
| Rust | `.rs`: functions, impl methods, mod/use | マクロ展開・トレイトのディスパッチなし / no macro expansion or trait dispatch |
| C | `.c`, `.h`: function definitions, `#include`, direct calls | プリプロセッサを実行しない。条件分岐の有効性や関数ポインタは未解決 / no preprocessing, active-branch or function-pointer resolution |
| JavaScript | `.js`, `.mjs`, `.cjs`: functions, arrows, methods, ESM/CommonJS imports | 動的プロパティ・実行時モジュール解決なし / no dynamic property or runtime module resolution |
| Python | `.py`: def, async def, methods, import/from import | 実行・モジュール読み込みなし。動的属性・デコレータの効果は未解決 / no execution or module loading; dynamic attributes and decorator effects unresolved |

## 保守的な解析 / Conservative analysis

追加言語の解析はコンパイラではなく、字句・構造に基づく初期対応です。同一ファイル内で静的に特定できる名前付き関数への直接呼び出しを接続し、それ以外は原則未解決として表示します。依存関係の抽出は呼び出し先の解決とは独立しています。型・オーバーロード・マクロ・FFI・言語間呼び出しの解決は行いません。構文全体の妥当性を保証せず、対応範囲外の宣言は一覧に含まれない場合があります。

Pythonのf/t文字列とJavaScript/TypeScriptのテンプレート補間の内部呼び出しは索引化しません。補間の境界を安全に判断できない場合、後続の宣言も索引化されないことがあります。\n\nCalls inside Python f/t strings and JavaScript/TypeScript template interpolation are not indexed. If interpolation boundaries are ambiguous, later declarations may also be omitted.\n\nAdded languages use lexical/structural indexing, not a compiler. Direct calls to provably named functions in the same file can be connected; other targets generally remain unresolved. Import extraction does not imply call resolution. Types, overloads, macros, FFI and cross-language calls are not resolved. This is not full syntax validation; unsupported declaration forms may be absent from the index.

Go以外ではGo標準ライブラリ・型・変数定義の解説を転用しません。構文解説は言語ごとの基本項目のみで、未対応のトークンには未対応と表示します。

Non-Go syntax help uses a language-specific basic catalog, never Go standard-library/type/variable explanations. Unsupported tokens are explicitly marked unsupported.

## AI送信 / AI transmission

文字列・コメントは言語別に同じバイト長の空白へ置換します（改行は保持）。未対応言語、字句解析に失敗したソース、解析時点と一致しない追加言語のソースでは送信準備を停止します。安全に区別できない構文は推測でマスクせず停止する場合があります。対象コードやPythonモジュールを実行・importすることはありません。

Language-aware masking replaces strings and comments with equal-length spaces while preserving newlines. Unsupported languages, lexical failures, and changed non-Go source snapshots block preparation. Ambiguous constructs may be rejected rather than guessed. No target code is executed or Python modules imported.

これは完全な機密情報除去ではありません。識別子・パス・ユーザーの指示は残り得るため、送信前のプレビューと除外設定を確認してください。

現在、Pythonのf/t文字列、JavaScript/TypeScriptのテンプレート補間、曖昧な正規表現境界などを含むファイルはAI送信準備を停止します。通常の文字列・Rustのraw文字列・Zigの複数行文字列・Cの行継続コメントなどは言語別にマスクします。\n\nCurrently, Python f/t strings, JavaScript/TypeScript template interpolation, and ambiguous regex boundaries block AI preparation for the file. Ordinary strings, Rust raw strings, Zig multiline strings and C line-spliced comments use language-aware masking.\n\nMasking is not complete secret removal: identifiers, paths and user instructions can remain. Audit the preview and exclusions before sending.
