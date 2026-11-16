# HodoCode user guide

[README](../../README.md) · [日本語](../ja/guide.md) · [Syntax explanation catalog](syntax-help.md)

## Start and analysis scope

HodoCode is a CLI and full-screen TUI for Go repositories. It produces dependency diagrams, function call diagrams, and short explanations with evidence. It supports local and GitHub repositories, including multiple Go modules. It has no external Go dependencies. Go 1.23 or later is required.

```sh
go build -o hodocode ./cmd/hodocode
./hodocode --help
```

`--root` is the repository or module directory to analyze. It defaults to the current directory. All nested `go.mod` files are detected. The root itself need not contain one. Use the repository root to cover the whole repository. Analysis and diagrams need neither network access nor fx. Check redirection destinations to avoid overwriting existing files.

## View diagrams and functions with the CLI

```sh
./hodocode deps --root /path/to/repo --dir internal --depth 1
./hodocode deps --root /path/to/repo > imports.mmd
./hodocode functions --root /path/to/repo
./hodocode flow --root /path/to/repo --func main
./hodocode flow --root /path/to/repo --func main --expand Run --expand Worker.Step
./hodocode tui --root /path/to/repo --func main
```

- `deps` writes a Mermaid diagram from files to imported packages to standard output.
- `--depth` is source directory depth relative to `--dir`. It is not the number of import hops. `0` means immediate files, `1` includes one level below, and `-1` includes all levels. Declarations are indexed across modules under `--root`, even outside the diagram's scope.
- `functions` lists function IDs, relative paths, and actual source line numbers.
- `--func` accepts a full function ID or a unique suffix. Ambiguity produces an error with candidates.
- Direct callees are resolved from declarations across all modules and packages under the root. Functions in other directories and nested modules remain selectable. A body is analyzed only when selected.
- `--expand` expands reachable functions one level at a time, in argument order. It does not recursively analyze every callee at once.
- `flow` is a call relationship diagram, not an if/loop control-flow diagram. Solid arrows mean statically resolved calls. Dashed arrows mean dynamic or unresolved calls.
- Functions under the root and in the local standard library can be expanded. Other external dependencies may appear but cannot be expanded. Import and call dependencies use separate commands and diagrams. Paste Mermaid output into a Markdown `mermaid` block to edit it.

## Full-screen TUI

Without `--func`, the TUI starts at the unique `main` function in `package main`. If none or several exist, it shows the reason or candidates and asks for `--func`. There is no start-function list pane.

The left two-thirds show the current function. The right third explains the cursor position. It covers Go syntax, operators, identifiers, and literals with examples. On whitespace, it chooses a nearby token on the same line. It separates the token, category, location, meaning, syntax, examples, cautions, and official references. Long explanations wrap.

These are Japanese template explanations from local parsing and built-in data. The main source is the [Go specification](https://go.dev/ref/spec). Viewing them does not contact the official site. Different uses of `*` and `<-` are distinguished. Loops, functions, arrays/slices, built-ins, and predeclared types have detailed categories. Known names such as `println`, `len`, and `string` are explained. There are dedicated explanations for 38 verified standard-library functions, including `fmt.Println` and `errors.Is`. Import aliases are supported. A custom function is not treated as standard merely because its name matches. See the [syntax explanation catalog](syntax-help.md) for coverage and editorial policy.

Code includes relative paths, line ranges, and actual source line numbers. Original notation is preserved. Tabs are expanded only for display. Control characters are not interpreted as terminal commands. Keywords, strings/runes, numbers, and comments have distinct colors. There is no type-based coloring. Only call names use muted cyan; method receivers are excluded. Arguments, parentheses, and comments receive no markers.

The header shows file position and right-pane state. The footer shows the mode, code position, and explanation scroll position. Hints adapt to terminal width and generation state. Additional instructions use a separate central panel.

### Keys

| Key | Action |
| --- | --- |
| `h/j/k/l`, arrows | Move one code character or line |
| `w` / `B` | Next / previous word |
| `0` / `$` | Start / end of line |
| `g` (`gg` also works) / `G` | Start / end of function |
| `PageUp` / `PageDown` | Move one screen |
| `n` / `N` | Next / previous call site in source order |
| `Enter` | Open the resolved call under the cursor |
| `b` | Return to the previous caller |
| `[` / `]` | Scroll the right pane up / down |
| `{` / `}` | Scroll only the variable popup up / down |
| `t` | Open the call tree |
| `v` / `r` | Open Neovim / explicitly reanalyze |
| `e` / `s` | Agent view / local syntax view |
| `i` / `E` | Enter instructions in agent view / generate immediately |
| `c` | Show / hide generated line explanations |
| `q`, `Ctrl+C`, `Ctrl+D` | Exit, with input and popup exceptions below |

These are Vim-style browsing controls, not an editor. There is no `Tab` pane switch or `d` detail toggle. Long code lines do not wrap. The viewport follows the cursor vertically and horizontally. The selected call name has a blue background and bold white text. Repeated calls to the same function are individually selectable. `b` restores the cursor, selection, and scroll position.

External and unresolved calls remain selectable. Yellow-toned underlined text distinguishes them. A reason explains missing source or static resolution failure. `Enter` does nothing on an unopenable call or outside a call name. The status shows selection number and source position. Recursive and previously analyzed functions use the cache without reanalysis. `NO_COLOR` removes color decoration from code, headers, and input panels.

### Call tree

`t` opens a central popup. Its root is the first function in navigation history, or the current function if there is no history. It initially shows the path to the current function and its direct children.

- `j/k` and up/down arrows only move selection. `Space` expands or collapses a branch. Analysis happens incrementally on expansion. Branch states survive selection changes.
- `Enter` opens code. `b` returns to the previous location.
- `Esc`, `q`, and `t` close only the popup and preserve code and explanation positions. `Ctrl+C/D` exits.
- The footer shows the full function ID, relative path, and line number. Cycles are not expanded further. Identical callees under one parent are merged. External and unresolved items remain with reasons but cannot be opened or expanded.
- Vertical scrolling, resizing, and `NO_COLOR` are supported.

### Read declarations and variables

A struct type name shows its type declaration, including fields. A slice variable shows its declaration and initialization. If its element is a named struct type, that definition is included. Lookup uses scope and type information across the project, local SDK, and locally available dependency sources.

The standard library supports declarations for functions and methods including bodies, types, constants, variables, and fields. Its functions and methods also support `Enter` navigation and tree expansion. Other external dependencies support only struct/slice displays in the right pane and variable popups. They cannot be opened or expanded.

On a variable name, declarations and references to the same variable in the displayed function share a highlight color. A popup above or below the cursor shows declaration code, file, and line number. Type analysis distinguishes shadowed variables and closures. Parameters, receivers, named results, and range variables are supported. Moving away from a variable clears the highlight. Unresolved identifiers are not highlighted by guesswork; an unavailable message is shown.

Source labels distinguish project-relative paths, `GOROOT/src/...`, module names and selected versions, `vendor`, and replacement paths. Actual line numbers are included. Unsupported or unresolved cases show a reason and syntax help. Displays use source snapshots only. They neither fetch nor infer runtime values, and send nothing externally. They do not overwrite an active agent conversation. Press `s` to return to the local view.

Display-only dependency resolution follows the referring module, active `go.work`, `replace`, `vendor`, and local module cache. Only needed packages are parsed lazily and reused within the process. SDK lookup uses the runtime `GOROOT` and the analysis OS, CPU, and build conditions. Read-only metadata comes from the local Go command. It does not access the network, fetch dependencies or toolchains, update dependency settings, build, test, or generate code. Missing Go commands or sources produce a reason and syntax help. Fetch missing dependencies separately. These extra sources are local-display-only. `functions` and AI transmission still cover only project functions.

### Terminals and fallback display

Full-screen mode supports macOS/Linux terminals. Resizing preserves the current function and selection. Below 41 columns by 8 rows, it asks for a larger terminal. Normal exit, `Ctrl+C`, and errors restore terminal input settings and the screen.

Redirecting either input or output, setting `TERM=dumb`, or using another OS selects line-input mode. Choose numbered items under the tree, return with `b`, and exit with `q` or EOF. Start-function selection also uses the existing line-input workflow. This mode never sends to AI. There are no mouse controls, apart from the AI-comment click behavior described below. Use `deps` and `flow` for diagrams.

## Neovim integration

In normal mode, `v` opens the current source in `nvim` at the same line and column. Exiting Neovim returns to the TUI with position and history preserved. Missing installations and launch failures show a reason and allow browsing to continue. Launch is disabled during search, search/instruction input, and tree display. GitHub-fetched remote sources cannot be edited. Use `--root` with a local clone to edit.

After saving, press `r` to reanalyze. There is no automatic refresh. Failure preserves the old display. If the current function was deleted, navigation falls back to a surviving history function, main, or another remaining function, and reports the destination. Reanalysis cancels generation and resets search. Conversation history survives, but old answers reflect source at generation time. Line explanations survive only when the function source, file, and line range are unchanged. Otherwise they are hidden with a prompt to regenerate using `E`.

To use an existing Neovim instance:

```sh
nvim --listen /tmp/hodocode-nvim
./hodocode tui --root /path/to/repo --nvim-server /tmp/hodocode-nvim
```

Only the explicitly configured endpoint receives the file and position. There is no automatic connection or fallback instance on connection failure. HodoCode keeps browsing, so switch to the connected Neovim terminal to edit. Files are passed as absolute-path arguments. Server connections use an escaped expression. No shell is involved.

## GitHub repositories

```sh
./hodocode functions --repo https://github.com/owner/repo --ref main
./hodocode flow --repo https://github.com/owner/repo --ref v1.0.0 --func main
./hodocode deps --repo https://github.com/owner/repo --subdir services/api
./hodocode tui --repo https://github.com/owner/repo
./hodocode explain --repo https://github.com/owner/repo --func main --purpose 'Check its role'
```

- `--repo` and `--root` are mutually exclusive. Only Go is supported. The target or a parent inside the repository must have `go.mod`. Multiple modules are supported.
- `--ref` accepts a branch, tag, or commit. It defaults to the default branch. Every run resolves it to a SHA through GitHub and displays the SHA. Use that SHA to reproduce a result. Function lists, call diagrams, explanation previews, and the TUI also show pinned source URLs with SHA and line numbers.
- `--subdir` narrows analysis only. It inherits parent modules but does not index out-of-scope sources. Paths outside the repository are rejected. It does not reduce download size.
- Fetching uses the GitHub API and source archives. It runs no Git, builds, scripts, or Git hooks. Dependencies are not fetched automatically. Archive links and unsafe paths are rejected.
- `--fetch-timeout` defaults to `2m`. `--max-bytes` defaults to `104857600` (100 MiB). They limit fetch time and compressed/extracted sizes. `Ctrl+C` cancels fetching. Temporary data from failed fetches is removed.
- Sources are cached by repository and SHA under `hodocode/repositories` in the OS user cache. Set the location with `--cache-dir PATH` and refetch with `--refresh`. Delete with `hodocode cache --repo URL --delete [--cache-dir PATH]`. Cached runs still need network access to resolve the SHA.
- Private repositories require `--private` and prior authentication through `gh auth login`. Existing gh credentials are used only for fetching, never included in logs or AI payloads. Private source remains in the cache. Delete it when no longer needed.
- Fetching and static analysis alone never send code to AI. `explain` defaults to a local preview and applies `--exclude`. Sending requires `--send` or an explicit full-screen TUI action.

## AI explanations and sending

fx must be installed and authenticated. Its settings determine the model, destination, and cost. Explanation accuracy is not guaranteed. Check the cited sources.

### Preview and send with the CLI

```sh
./hodocode explain --root /path/to/repo --func Run \
  --purpose 'Understand its role and processing flow' \
  --exclude 'internal/auth' --exclude '*_generated.go'
```

This locally lists transmission candidates. It does not start fx. Check the purpose, candidate files, code, and fx settings. Then add `--send --timeout 2m` to the same arguments.

There are two communication stages.

1. Send the purpose, allowed function metadata, and one level of calls from the starting function. The agent selects at most 12 function IDs.
2. Validate the selection against the allowlist. Send only the selected functions' processed code, positions, and relationships. Request a short Japanese explanation of roles, flow, and cautions, citing files and lines and separating facts from inference.

Invalid IDs or JSON, duplicates, and oversized input produce errors. They are never interpreted as arbitrary paths or commands. The connection is isolated behind `internal/explain.Runner` and can be replaced without changing analysis.

### TUI generation and conversation

`tui` also accepts `--purpose`, `--exclude`, and `--timeout`. The default purpose covers roles, flow, syntax, and cautions for beginners. The default timeout is two minutes. Exclusion, blanking, and size checks match the CLI. Skipping a TUI confirmation does not change fx permissions. CLI `explain --send` behavior is unchanged.

- `e` only displays the conversation. It starts neither input nor sending. Repeated presses preserve the conversation. Switching during generation retains progress hints.
- `i` opens input only in the conversation view. Input is tied to the function displayed when opened. Opening alone does not send. Japanese input, trailing deletion with `Backspace`, clearing with `Ctrl+U`, and cancellation with `Esc` are supported. `q` is text. `Ctrl+C/D` exits.
- `Enter` in input sends immediately. There is no `y/n` confirmation. A question is limited to 8KiB. Past questions, answers, and target function IDs are sent separately from the purpose. Watch for secrets in questions and answers too.
- `E` immediately generates an explanation of the current function without opening input. Even after an answer, it appends to the conversation rather than replacing past answers.
- Only one generation runs at once. During generation, `i` and `E` start nothing new and show which keys to use afterward. After failure, re-enter a question with `i` or regenerate with `E`.
- Follow-up answers address only the question. A full revision requires an explicit request. Ordinary questions do not change line comments. Only a successful `/comments revision instructions` request or `E` replaces them. Failure preserves old comments.

The right pane shows the summary. The left pane shows line details directly below target code. “AI explanation” entries are specially colored virtual comments. They change neither source files nor original line numbers. Long text wraps to the left-pane width. Multi-line explanations appear below their last target line. Explanations with unverifiable files, functions, or line ranges stay in the right pane. `c` only shows or hides all generated line explanations. It neither regenerates nor sends. It is independent of `e/s` on the right. Clicking a comment selects its code line. Navigation, search, call navigation, and Neovim use original source positions.

Code navigation remains available during generation. Results belong to the function selected when generation started, never to a newly visited function. Returning to the original function shows its line explanations. Exiting cancels generation. Summaries and errors appear on the right and scroll with `[` / `]`.

Conversation entries are appended chronologically with speaker and target function ID. One session shares context across function navigation. New answers show their beginning; older entries remain scrollable. Progress and errors are separate from conversation text. Cancellation and communication errors preserve past answers. `s` changes only the view. It preserves history and separate syntax/conversation scroll positions. History disappears on exit and is not saved to disk.

### Safety checks before sending

- Candidates include non-excluded repository functions beyond the current function. Audit candidate files before sending.
- `--exclude` uses relative-path `filepath.Match` patterns. They apply to files and each directory prefix. This is not recursive `**` globbing. Excluded files' function metadata and related local edges are also omitted.
- Path components containing terms such as `secret`, `credential`, `token`, `key`, `private`, or `.env` are automatically excluded. Harmless files may also be excluded.
- All string/rune literals and comments are blanked. Actual source line numbers are preserved. This prevents explanation of string meaning.
- Exclusion and blanking do not guarantee complete secret detection. Identifiers, numbers, filenames, module names, and purpose text may contain secrets. The preview lists candidates; it is not the entire processed code. Add necessary exclusions and do not modify files during preparation.
- fx starts in an empty temporary directory as `fx ask --no-save --no-color`. Instructions prohibit additional access through tools, files, or networks. This is not an OS sandbox. Do not send if fx or its configuration is untrusted, or if a sensitive repository requires strict isolation.
- Limits are 4MiB per file, 8MiB total processed code, and 128KiB per prompt. History and code for up to 12 potentially selected functions are conservatively checked before sending. Overflow stops the request with a reason; history and code are never silently truncated.

## Analysis limits and evaluation

These limits concern project analysis and indexing, not display-only lookup of local SDK and dependency sources.

- Analysis covers only areas with `go.mod` under `--root`. Hidden files/directories, `vendor`, `_test.go`, symlink sources, and Go files outside modules are excluded.
- Project analysis does not reproduce `go.work` or `require/replace` version and resolution order. It resolves by module names and existing directories. Replacements with different module names are unsupported. Out-of-repository replacements and external dependencies are not loaded into project analysis. Duplicate module names or package paths produce errors instead of guesses.
- OS/CPU-specific files and build constraints follow the analysis CLI's `go/build.Default`. Custom analysis build tags are unsupported. Duplicate declarations in included files produce errors.
- Indexing requires syntax parsing of target Go files. Body type analysis and call expansion are limited to selected functions.
- Interface calls, function values, and calls without sufficient type information remain unknown. Concrete-type methods resolve when statically identifiable. Dependencies other than the standard library are not fetched. Closure bodies are not expanded.

```sh
go test ./cmd/hodocode ./internal/analyze ./internal/explain
go run ./cmd/hodocode flow --root testdata/analyze --func CycleA --expand CycleB
go run ./cmd/hodocode tui --root testdata/analyze --func Recur
```

`testdata/analyze` contains imports, calls in branches, cycles, and dynamic calls. It includes intentionally unresolved symbols and is not an executable program. Tests use no external model. Fake fx executables and fixed responses verify transmission scope and error handling.
