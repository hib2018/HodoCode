package main

import (
	"go/token"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/hib2018/HodoCode/internal/sourcelex"
)

func languageExplanations(language, source string, symbols ...map[int]string) []syntaxExplanation {
	if language == "" || language == "go" {
		return sourceExplanations(source, symbols...)
	}
	tokens, err := sourcelex.Scan(language, source)
	if err != nil {
		lines := strings.Split(source, "\n")
		return []syntaxExplanation{{endRow: len(lines) - 1, endColumn: utf8.RuneCountInString(lines[len(lines)-1]), topic: "language:" + language + ":unsupported"}}
	}
	starts := []int{0}
	for i := range source {
		if source[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	position := func(offset int) (int, int) {
		row := sort.Search(len(starts), func(i int) bool { return starts[i] > offset }) - 1
		return row, utf8.RuneCountInString(source[starts[row]:offset])
	}
	var notes []syntaxExplanation
	for _, tok := range tokens {
		if tok.Kind == "comment" {
			continue
		}
		row, column := position(tok.Start)
		endRow, endColumn := position(tok.End)
		kind := tok.Kind
		if kind == "identifier" && languageKeyword(language, tok.Text) {
			kind = "keyword"
		}
		// Never populate Go token/topic metadata for another language.
		notes = append(notes, syntaxExplanation{row, column, endRow, endColumn, token.ILLEGAL, tok.Text, "language:" + language + ":" + kind})
	}
	return notes
}

func languageHelp(note syntaxExplanation) syntaxHelp {
	parts := strings.SplitN(strings.TrimPrefix(note.topic, "language:"), ":", 2)
	language, kind := parts[0], parts[1]
	info := map[string]struct{ name, reference, example string }{
		"zig":        {"Zig", "https://ziglang.org/documentation/master/", "fn twice(value: i32) i32 {\n    const result = value * 2;\n    return result;\n}"},
		"c":          {"C", "https://en.cppreference.com/w/c/language", "int twice(int value) {\n    int result = value * 2;\n    return result;\n}"},
		"javascript": {"JavaScript", "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference", "function twice(value) {\n  const result = value * 2;\n  return result;\n}"},
		"typescript": {"TypeScript", "https://www.typescriptlang.org/docs/handbook/2/everyday-types.html", "function twice(value: number): number {\n  const result: number = value * 2;\n  return result;\n}"},
		"python":     {"Python", "https://docs.python.org/3/reference/", "def twice(value):\n    result = value * 2\n    return result"},
		"rust":       {"Rust", "https://doc.rust-lang.org/reference/", "fn twice(value: i32) -> i32 {\n    let result = value * 2;\n    result\n}"},
	}[language]
	help := syntaxHelp{category: info.name + " > 構文", title: note.literal, syntax: note.literal, example: info.example, caution: "字句レベルの説明です。型・スコープ・実行時の動作は周辺のコードも確認してください。", reference: info.reference}
	if info.name == "" || kind == "unsupported" {
		help.title = "構文解説未対応"
		help.description = language + " の構文解説を利用できません。未対応の言語または安全に字句解析できないソースです。"
		return help
	}
	switch kind {
	case "identifier":
		help.description = info.name + " の変数・関数・型などの名前です。宣言位置とスコープで参照先が決まります。"
	case "string":
		help.description = info.name + " の文字列または文字リテラルです。引用符・接頭辞によってエスケープや補間の規則が異なります。"
	case "number":
		help.description = info.name + " の数値リテラルです。表記と使用箇所によって型や精度が決まります。"
	case "keyword":
		help.description = info.name + " の構文キーワードです。このキーワードの詳細な解説は未対応です。"
	case "punct":
		help.description = info.name + " の演算子または区切り記号です。この記号の詳細な解説は未対応です。"
	default:
		help.description = info.name + " のこの構文の詳細な解説は未対応です。"
	}
	switch language + ":" + note.literal {
	case "javascript:function", "typescript:function":
		help.description = "function は名前・引数・本体を持つ関数を定義します。引数と戻り値を使って処理をまとめます。"
	case "javascript:const", "typescript:const":
		help.description = "const は再代入できないブロックスコープの束縛を宣言します。オブジェクトの内容まで不変になるわけではありません。"
	case "javascript:let", "typescript:let":
		help.description = "let は再代入可能なブロックスコープの変数を宣言します。"
	case "typescript:interface", "typescript:type":
		help.description = "TypeScript の型を宣言します。型の情報は通常 JavaScript 出力から消え、実行時の検証にはなりません。"
	case "python:def":
		help.description = "def は関数を定義します。コロンの後のインデントされたブロックが関数の本体です。"
	case "python:None":
		help.description = "None は値がないことを表す Python の単一のオブジェクトです。判定には通常 is None を使います。"
	case "python:for":
		help.description = "for はイテラブルから要素を順に取り出します。コロンとインデントで繰り返す本体を記述します。"
	case "zig:fn":
		help.description = "fn は関数を定義します。引数と戻り値の型を指定し、波括弧の中に本体を書きます。"
	case "zig:const":
		help.description = "const は再代入できない束縛を宣言します。コンパイル時の定数であるとは限りません。"
	case "zig:var":
		help.description = "var は変更可能な変数を宣言します。型は明示するか初期値から推論させます。"
	case "c:int":
		help.description = "int は符号付き整数型です。正確な範囲は処理系によって異なり、符号付き整数のオーバーフローは未定義動作です。"
	case "c:static":
		help.description = "static は宣言位置に応じて内部リンケージや静的記憶域期間などを指定します。ファイル内の関数では外部からの参照を制限します。"
	case "rust:fn":
		help.description = "fn は関数を定義します。引数には型を指定し、戻り値の型は -> の後に書きます。"
	case "rust:let":
		help.description = "let はパターンに値を束縛します。束縛は既定で不変で、再代入を許可するには mut を付けます。"
	case "rust:mut":
		help.description = "mut は束縛や参照に可変性を指定します。所有権と借用の規則にも従う必要があります。"
	case "rust:match":
		help.description = "match は値をパターンと照合して分岐する式です。すべての可能性を扱う必要があります。"
	case "javascript:return", "typescript:return", "python:return", "rust:return", "zig:return", "c:return":
		help.description = info.name + " の関数から呼び出し元へ制御を戻し、指定された値を返します。"
	}
	return help
}
