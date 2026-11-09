package main

import (
	"fmt"
	"go/scanner"
	"go/token"
)

type syntaxExplanation struct {
	row   int
	token token.Token
}

var syntaxDescriptions = map[token.Token]string{
	token.FUNC:        "func: 関数やメソッドを定義します。",
	token.RETURN:      "return: 関数を終了し、戻り値を返します。",
	token.IF:          "if: 条件が真のときに処理を実行します。",
	token.ELSE:        "else: ifの条件が偽のときの処理です。",
	token.FOR:         "for: 条件や範囲に従って処理を繰り返します。",
	token.RANGE:       "range: 配列・スライス・mapなどを順に処理します。整数の回数指定やチャネルの受信にも使います。",
	token.SWITCH:      "switch: 値や条件に応じて処理を分岐します。",
	token.CASE:        "case: switchやselectの分岐を指定します。",
	token.DEFAULT:     "default: 他の分岐が該当しない場合の処理です。",
	token.SELECT:      "select: 実行可能なチャネルの送受信を選びます。",
	token.DEFER:       "defer: 関数終了時に呼び出しを実行します。複数ある場合は後に登録したものから実行します。",
	token.GO:          "go: 関数呼び出しを別のゴルーチンで実行します。",
	token.VAR:         "var: 変数を宣言します。",
	token.CONST:       "const: 定数を宣言します。",
	token.TYPE:        "type: 型を定義するか、別名を宣言します。",
	token.STRUCT:      "struct: 複数のフィールドをまとめる構造体です。",
	token.INTERFACE:   "interface: メソッドや型の条件を表します。",
	token.MAP:         "map: キーと値の組を保持する型です。",
	token.CHAN:        "chan: ゴルーチン間で値を送受信するチャネルです。",
	token.BREAK:       "break: ループ・switch・selectを抜けます。",
	token.CONTINUE:    "continue: ループの次の反復へ進みます。",
	token.GOTO:        "goto: 指定したラベルへ移動します。",
	token.FALLTHROUGH: "fallthrough: switchの次のcaseの処理へ進みます。",
	token.DEFINE:      ":=: 型を推論して変数を宣言・代入します。同じスコープに新しい変数が必要です。",
	token.ARROW:       "<-: チャネルの送受信や、チャネル型の方向を表します。",
	token.ELLIPSIS:    "...: 可変長引数・スライスの引数展開・配列長の推論に使います。",
}

func sourceExplanations(source string) []syntaxExplanation {
	file := token.NewFileSet().AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, []byte(source), func(token.Position, string) {}, 0)
	var notes []syntaxExplanation
	for {
		pos, tok, _ := scan.Scan()
		if tok == token.EOF {
			return notes
		}
		if syntaxDescriptions[tok] != "" {
			notes = append(notes, syntaxExplanation{row: file.Position(pos).Line - 1, token: tok})
		}
	}
}

func (s *screen) explanationLines(width, height int) []string {
	lines := []string{"構文の解説", "表示中のコード（外部送信なし）"}
	seen := map[token.Token]bool{}
	for _, note := range s.explanations {
		if note.row < s.top || note.row >= s.top+height || seen[note.token] {
			continue
		}
		seen[note.token] = true
		text := fmt.Sprintf("%d行: %s", s.p.Functions[s.current].Line+note.row, syntaxDescriptions[note.token])
		lines = append(lines, "")
		line := ""
		for _, r := range text {
			if displayWidth(line)+runeWidth(r) > width {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
		lines = append(lines, line)
	}
	if len(seen) == 0 {
		lines = append(lines, "", "解説対象の構文はありません。")
	}
	return lines
}

func screenCodeWidth(width int) int {
	return width - width/3 - 3
}
