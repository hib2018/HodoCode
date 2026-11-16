# Go syntax help catalog

[Guide](guide.md) | [日本語](../ja/syntax-help.md)

The right pane is a small textbook based mainly on the official Go language specification. Its explanations are independently summarized in Japanese. They are not full translations of official documents or agent-generated results. The explanations are embedded in the binary. Viewing them makes no network requests and sends no data outside the application.

## Entry structure

1. Token under the cursor and its source position
2. Category and topic name (for example, Types > Pointers or Expressions > Dereferencing)
3. Meaning: what it represents and how it behaves
4. Syntax: how to write it
5. Example: Go code with line breaks and indentation, plus comments about results or behavior
6. Caveats: nil, out-of-range access, sharing, type differences, version differences, and more
7. Official reference: the URL of the relevant specification section

Scroll long explanations with `[` / `]`. URLs appear as text. They do not open automatically.

## Categories and main references

| Area | Example topics | Official references |
| --- | --- | --- |
| Declarations and names | Variables, constants, short variable declarations, type definitions and aliases, scope | [Declarations and scope](https://go.dev/ref/spec#Declarations_and_scope) |
| Functions | Named functions, methods, function literals, function types, variadic arguments | [Function declarations](https://go.dev/ref/spec#Function_declarations), [Function types](https://go.dev/ref/spec#Function_types) |
| Control flow | Conditionals, expression switches and type switches, the three forms of for, range | [Statements](https://go.dev/ref/spec#Statements), [For statements](https://go.dev/ref/spec#For_statements) |
| Types and composite values | Pointers, arrays, slices, maps, structs, interfaces, type arguments | [Types](https://go.dev/ref/spec#Types), [Instantiations](https://go.dev/ref/spec#Instantiations) |
| Expressions | Arithmetic, logical and bitwise operations, address-taking, dereferencing, indexing, slicing, type assertions | [Expressions](https://go.dev/ref/spec#Expressions) |
| Concurrency | go, defer, channel direction, sending and receiving, select | [Channel types](https://go.dev/ref/spec#Channel_types), [Select statements](https://go.dev/ref/spec#Select_statements) |
| Built-ins | len/cap, make/new, append/copy, delete/clear, close, panic/recover, and more | [Built-in functions](https://go.dev/ref/spec#Built-in_functions) |
| Lexical elements | Numbers, runes, interpreted and raw string literals, delimiters | [Lexical elements](https://go.dev/ref/spec#Lexical_elements) |

Behavior is checked against the language specification. [Effective Go](https://go.dev/doc/effective_go) is a supplementary source for idiomatic explanations. It is not being updated and does not cover generics and other features. The language specification takes priority for newer features.

The reference checked here is the published Go 1.27 specification. Entries state the Go version required for new syntax and built-ins. The catalog does not hide entries based on the target project's `go.mod`.

## Scope of syntax detection

- Cursor positions use physical lines and rune columns in the original source. They are not replaced with virtual positions from `//line` directives.
- In addition to scanning tokens, the current function fragment is parsed into an AST. This distinguishes cases such as pointer types (`*T`), dereferencing (`*p`), and multiplication (`a*b`).
- Built-in functions such as `println` and `len`, and predeclared types such as `string` and `error`, are explained by name. This also applies outside normal call positions. Same-name declarations, field names, and selectors within the fragment are distinguished from ordinary identifiers. Declarations outside the fragment cannot be resolved from syntax alone. Caveats identify these as candidates and note possible same-name declarations.
- There are dedicated explanations for 38 standard library functions. These cover commonly used functions in `fmt`, `errors`, `strings`, `strconv`, `os`, `io`, `context`, `time`, and `log`. Matching uses the package path and function name returned by the existing call analysis. Import aliases are supported. A custom method merely written as `fmt.Println` does not match. When analysis identifies a custom function or function value, it also suppresses built-in help with the same name.
- References for standard functions are the [official Go standard library reference](https://pkg.go.dev/std) and `go doc` from the installed Go toolchain. For example, built-in `println` provides implementation-dependent debug output. [fmt.Println](https://pkg.go.dev/fmt#Println) writes to standard output and returns a byte count and an error. The explanations distinguish these behaviors. Links are displayed only; no automatic connection is made.
- Syntax alone cannot tell whether `f[T]` in an expression is indexing or a type argument. Both possibilities are explained unless the context is conclusive, such as a type position.
- Incomplete fragments and fragments with syntax errors skip AST detection. They fall back to token-level explanations. Comment contents are not explained as code.

## Updates and verification

Explanation data is in `cmd/hodocode/syntax_help.go`. Standard function explanations are in `standard_help.go`, context detection in `syntax_context.go`, and rendering in `syntax_explanation.go`.

New entries need all seven data fields. They also need a reference to the official specification section or standard library symbol that explains the rule. Do not guess meanings that cannot be determined. State the limits in the caveats.

```sh
go test ./cmd/hodocode -run 'Test(Known|StandardHelp|SourceTopics|SourceExplanations|SyntaxExplanation|SyntaxHelp)'
go test -race ./cmd/hodocode
```

The tests check:

- Representative categories and same-name declarations
- Unicode positions and wrapping in narrow panes
- Required catalog fields
- Syntax and type checking for all examples
- Execution results for selected examples

The wording of meanings and caveats still needs human review against official references.
