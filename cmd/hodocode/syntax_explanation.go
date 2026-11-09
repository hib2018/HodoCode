package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"strings"
	"unicode/utf8"
)

type syntaxExplanation struct {
	row, column, endRow, endColumn int // Zero-based rune positions; end is exclusive.
	token                          token.Token
	literal                        string
}

type syntaxHelp struct {
	description, example string
}

var syntaxDescriptions = map[token.Token]syntaxHelp{
	token.FUNC:        {"関数やメソッドを定義します。引数と戻り値の型を指定できます。", "func add(a, b int) int { return a + b }"},
	token.RETURN:      {"現在の関数を終了し、宣言された戻り値を呼び出し元へ返します。", "return result, nil"},
	token.IF:          {"bool型の条件が真のときだけ処理します。条件の前に短い初期化文も書けます。", "if n := len(items); n > 0 { println(n) }"},
	token.ELSE:        {"ifの条件が偽のときの処理です。else ifで条件を追加できます。", "if ok { save() } else { retry() }"},
	token.FOR:         {"条件・初期化と更新・rangeで処理を繰り返します。条件を省くと無限ループです。", "for i := 0; i < 3; i++ { println(i) }"},
	token.RANGE:       {"配列・スライス・文字列・mapなどを順に処理します。文字列ではバイト位置とruneを返します。整数やチャネルにも使えます。", "for i, value := range items { println(i, value) }"},
	token.SWITCH:      {"値や条件に一致するcaseへ分岐します。通常は一致したcaseだけを実行します。", "switch n { case 0: println(\"zero\"); default: println(n) }"},
	token.CASE:        {"switchの一致条件、またはselectのチャネル操作を指定します。", "select { case value := <-ch: println(value) }"},
	token.DEFAULT:     {"他のcaseが該当しないときの処理です。selectでは待機せずこの分岐へ進みます。", "select { case ch <- 1: default: println(\"busy\") }"},
	token.SELECT:      {"実行可能なチャネル送受信を選びます。複数あれば擬似ランダムに選び、なければ待機します。", "select { case value := <-ch: println(value) }"},
	token.DEFER:       {"関数終了時に呼び出しを実行します。引数は登録時に評価し、複数の呼び出しは逆順で実行します。", "defer file.Close()"},
	token.GO:          {"関数呼び出しを別のゴルーチンで並行実行します。完了を待つには別途同期が必要です。", "go work()"},
	token.VAR:         {"変数を宣言します。初期値を省くと型のゼロ値になります。", "var count int // 初期値は0"},
	token.CONST:       {"コンパイル時に決まる定数を宣言します。宣言後に変更できません。", "const limit = 10"},
	token.TYPE:        {"新しい型を定義します。=を使うと既存の型の別名を宣言します。", "type UserID int; type ID = int"},
	token.STRUCT:      {"名前と型を持つフィールドをまとめる構造体です。", "type User struct { Name string; Age int }"},
	token.INTERFACE:   {"必要なメソッドを表します。型制約として型の集合を指定する用途もあります。", "type Reader interface { Read([]byte) (int, error) }"},
	token.MAP:         {"キーと値の組を保持する型です。nilのmapへの書き込みはパニックになります。", "scores := map[string]int{\"Go\": 100}"},
	token.CHAN:        {"ゴルーチン間で値を送受信するチャネル型です。バッファなしの送受信は相手を待ちます。", "ch := make(chan int); go func() { ch <- 1 }()"},
	token.BREAK:       {"最も内側のfor・switch・selectを抜けます。ラベルを指定して外側を抜けることもできます。", "for { if done { break } }"},
	token.CONTINUE:    {"現在のforの残りを飛ばし、次の反復へ進みます。", "for _, n := range nums { if n < 0 { continue }; println(n) }"},
	token.GOTO:        {"同じ関数内のラベルへ移動します。変数宣言を飛び越すなどの制約があります。", "goto done; done: return"},
	token.FALLTHROUGH: {"式switchで次のcaseの条件を調べずに処理へ進みます。型switchでは使えません。", "switch n { case 1: println(1); fallthrough; case 2: println(2) }"},
	token.PACKAGE:     {"このファイルが属するパッケージ名を宣言します。", "package main"},
	token.IMPORT:      {"別のパッケージを読み込み、その公開名を利用できるようにします。", "import \"fmt\""},
	token.DEFINE:      {"型を推論して変数を宣言・代入します。関数内で使い、同じスコープに少なくとも1つ新しい変数が必要です。", "count := 1"},
	token.ASSIGN:      {"宣言済みの変数などに値を代入します。比較は==を使います。", "count = 2"},
	token.ADD:         {"数値の加算、文字列の連結、または単項の正符号です。", "total := 1 + 2; name := \"Go\" + \"pher\""},
	token.SUB:         {"数値の減算、または単項の負符号です。", "difference := a - b; negative := -1"},
	token.MUL:         {"数値の乗算、ポインタ型、またはポインタの参照先へのアクセスを表します。", "product := a * b; var p *int; _ = *p"},
	token.QUO:         {"数値を除算します。整数同士の結果は小数部分をゼロ方向に切り捨てます。", "n := 7 / 2 // 3"},
	token.REM:         {"整数の除算の余りです。結果の符号は割られる数に従います。", "remainder := 7 % 3 // 1"},
	token.AND:         {"整数のビットAND、または値のアドレスを取得する単項演算子です。", "bits := 6 & 3; p := &value"},
	token.OR:          {"整数の各ビットのORを計算します。型制約では型の和集合にも使います。", "bits := 4 | 1 // 5"},
	token.XOR:         {"二項ではビットXOR、単項ではビット反転です。", "bits := 6 ^ 3; inverted := ^uint(0)"},
	token.SHL:         {"整数のビットを左へシフトします。", "n := 1 << 3 // 8"},
	token.SHR:         {"整数のビットを右へシフトします。", "n := 8 >> 2 // 2"},
	token.AND_NOT:     {"右側で1のビットを左側から取り除きます。", "bits := 7 &^ 2 // 5"},
	token.LAND:        {"両方が真なら真です。左が偽なら右は評価しません。", "if p != nil && p.Ready { work() }"},
	token.LOR:         {"どちらかが真なら真です。左が真なら右は評価しません。", "if cached || load() { use() }"},
	token.NOT:         {"bool値の真偽を反転します。", "if !ready { return }"},
	token.EQL:         {"比較可能な値が等しいか調べ、bool値を返します。", "if count == 0 { return }"},
	token.NEQ:         {"比較可能な値が異なるか調べ、bool値を返します。", "if err != nil { return err }"},
	token.LSS:         {"左が右より小さいか比較します。", "if n < limit { work() }"},
	token.LEQ:         {"左が右以下か比較します。", "if n <= limit { work() }"},
	token.GTR:         {"左が右より大きいか比較します。", "if n > 0 { work() }"},
	token.GEQ:         {"左が右以上か比較します。", "if n >= 0 { work() }"},
	token.INC:         {"変数を1増やす文です。式ではないため代入の右辺には使えません。", "count++"},
	token.DEC:         {"変数を1減らす文です。式ではないため代入の右辺には使えません。", "count--"},
	token.ARROW:       {"チャネルの送受信や、チャネル型の方向を表します。送受信は準備ができるまで待機します。", "ch <- value; value = <-ch; var input <-chan int"},
	token.ELLIPSIS:    {"可変長引数・スライスの引数展開・配列長の推論に使います。", "func sum(ns ...int) {}; sum(values...); _ = [...]int{1, 2}"},
	token.TILDE:       {"型制約で、基底型が指定した型である型の集合を表します。", "type Integer interface { ~int }"},
	token.INT:         {"整数リテラルです。0xは16進、0bは2進、0oは8進を表し、_で桁を区切れます。", "count := 1_000; mask := 0xff"},
	token.FLOAT:       {"浮動小数点リテラルです。小数点や指数表記を使えます。", "ratio := 1.5; scale := 1e3"},
	token.IMAG:        {"末尾のiで虚数を表します。実部と組み合わせて複素数を作れます。", "z := 1 + 2i"},
	token.CHAR:        {"単一引用符で囲むruneリテラルです。Unicodeコードポイントを表す整数値です。", "letter := 'あ'; newline := '\\n'"},
	token.STRING:      {"文字列リテラルです。二重引用符ではエスケープを解釈し、バッククォートでは改行などをそのまま含めます。", "message := \"こんにちは\\n\"; path := `a\\b`"},
	token.IDENT:       {"変数・関数・型・パッケージなどの名前です。宣言とスコープで意味が決まります。大文字で始まる名前はパッケージ外へ公開できます。", "value := 1; println(value)"},
	token.LPAREN:      {"引数・パラメータや、式をまとめる括弧の始まりです。", "result := add(1, 2 * (3 + 4))"},
	token.RPAREN:      {"引数・パラメータや、式をまとめる括弧の終わりです。", "println(value)"},
	token.LBRACK:      {"添字・スライス・配列型・型引数などの角括弧の始まりです。", "first := items[0]; part := items[1:3]"},
	token.RBRACK:      {"添字・スライス・配列型・型引数などの角括弧の終わりです。", "var values []int"},
	token.LBRACE:      {"処理ブロック、複合リテラルや型定義の波括弧の始まりです。", "if ok { println(\"ok\") }"},
	token.RBRACE:      {"処理ブロック、複合リテラルや型定義の波括弧の終わりです。", "values := []int{1, 2}"},
	token.COMMA:       {"引数・要素・複数の名前や値などを区切ります。", "a, b := 1, 2"},
	token.PERIOD:      {"パッケージの公開名やフィールド・メソッドを選択します。型アサーションにも使います。", "fmt.Println(user.Name); value := x.(string)"},
	token.COLON:       {"ラベル、case、複合リテラルのキー、スライスの範囲などを区切ります。", "values := map[string]int{\"a\": 1}; part := items[1:3]"},
	token.SEMICOLON:   {"文を区切ります。通常は改行時に自動挿入されます。", "for i := 0; i < 3; i++ {}"},
}

func sourceExplanations(source string) []syntaxExplanation {
	file := token.NewFileSet().AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, []byte(source), func(token.Position, string) {}, 0)
	// Compute columns from raw bytes, not scanner columns (which count bytes).
	lineStarts := []int{0}
	for i, b := range []byte(source) {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	position := func(offset int) (int, int) {
		p := file.PositionFor(file.Pos(offset), false)
		return p.Line - 1, utf8.RuneCountInString(source[lineStarts[p.Line-1]:offset])
	}
	var notes []syntaxExplanation
	for {
		pos, tok, literal := scan.Scan()
		if tok == token.EOF {
			return notes
		}
		if tok == token.ILLEGAL || tok == token.SEMICOLON && literal == "\n" {
			continue
		}
		if literal == "" {
			literal = tok.String()
		}
		offset := file.Offset(pos)
		end := offset + len(literal)
		// The scanner removes CR bytes from raw string literals; retain the actual span.
		if tok == token.STRING && source[offset] == '`' {
			if closing := strings.IndexByte(source[offset+1:], '`'); closing >= 0 {
				end = offset + closing + 2
			} else {
				end = len(source)
			}
		}
		row, column := position(offset)
		endRow, endColumn := position(min(end, len(source)))
		notes = append(notes, syntaxExplanation{row, column, endRow, endColumn, tok, literal})
	}
}

func (note syntaxExplanation) help() syntaxHelp {
	if note.token.IsOperator() && note.token >= token.ADD_ASSIGN && note.token <= token.AND_NOT_ASSIGN {
		op := strings.TrimSuffix(note.token.String(), "=")
		return syntaxHelp{"演算結果を左側へ代入します。左側は一度だけ評価されます。", "x " + note.token.String() + " y // x = x " + op + " y に相当"}
	}
	if note.token == token.IDENT {
		switch note.literal {
		case "true", "false":
			return syntaxHelp{"事前宣言されたbool型の真偽値です。通常、条件式やフラグに使います。", "ready := true; if !ready { return }"}
		case "nil":
			return syntaxHelp{"ポインタ・スライス・map・チャネル・関数・interfaceの値がない状態を表します。型によって操作の可否が異なります。", "if err != nil { return err }"}
		case "_":
			return syntaxHelp{"空白識別子です。不要な値を捨て、通常の変数のようには参照できません。", "value, _ := lookup()"}
		case "iota":
			return syntaxHelp{"const宣言内で0から始まり、宣言の行ごとに増える事前宣言された定数です。", "const ( first = iota; second ) // 0, 1"}
		}
	}
	return syntaxDescriptions[note.token]
}

func (s *screen) explanationLines(width, height int) []string {
	lines := []string{"構文の解説", "カーソル位置（外部送信なし）"}
	var selected *syntaxExplanation
	best := int(^uint(0) >> 1)
	if height > 0 && s.cursorRow >= s.top && s.cursorRow < s.top+height {
		for i := range s.explanations {
			note := &s.explanations[i]
			if s.cursorRow < note.row || s.cursorRow > note.endRow || note.help().description == "" {
				continue
			}
			start, end := 0, int(^uint(0)>>1)
			if s.cursorRow == note.row {
				start = note.column
			}
			if s.cursorRow == note.endRow {
				end = note.endColumn
			}
			if start <= s.cursorColumn && s.cursorColumn < end {
				selected = note
				break
			}
			distance := start - s.cursorColumn
			if s.cursorColumn >= end {
				distance = s.cursorColumn - end + 1
			}
			// Equal distances prefer the preceding token, matching reading order.
			if distance < best {
				selected, best = note, distance
			}
		}
	}
	texts := []string{"解説対象の構文はありません。"}
	if selected != nil {
		help := selected.help()
		// Quote literals so embedded newlines and tabs cannot disrupt the pane.
		label := selected.literal
		if selected.token.IsLiteral() || selected.token == token.IDENT {
			label = fmt.Sprintf("%q", label)
		}
		texts = []string{
			fmt.Sprintf("%d行 %d列: %s: %s", s.p.Functions[s.current].Line+s.cursorRow, s.cursorColumn+1, label, help.description),
			"例: " + help.example,
		}
	}
	width = max(1, width)
	for _, text := range texts {
		lines = append(lines, "")
		line := ""
		for _, r := range text {
			if runeWidth(r) > width {
				r = '?'
			}
			if displayWidth(line)+runeWidth(r) > width {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
		lines = append(lines, line)
	}
	return lines
}

func screenCodeWidth(width int) int {
	return width - width/3 - 3
}
