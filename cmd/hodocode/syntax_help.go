package main

import (
	"go/token"
	"strings"
)

// The catalog paraphrases the Go specification; references identify each rule.
// Builtin topics describe syntax only: the caller must account for shadowing.
type syntaxHelp struct {
	category, title, description, syntax, example, caution, reference string
}

var syntaxDescriptions = map[token.Token]syntaxHelp{
	token.ILLEGAL: {
		category: "字句 > エラー", title: "認識できない文字",
		description: "Goの字句として認識できない文字です。全角の記号や閉じ忘れたリテラルが原因になることがあります。",
		syntax:      "Goで定義された文字・トークンを使う", example: "n := 1\nprintln(n) // 1",
		caution: "見た目が似ていても全角の引用符や演算記号は代用できません。", reference: "https://go.dev/ref/spec#Tokens",
	},
	token.EOF: {
		category: "字句 > 終端", title: "ソースの終わり",
		description: "ソースの終端です。Goの文として記述するキーワードではありません。",
		syntax:      "最後の宣言・文を閉じてファイルを終える", example: "func end() {\n\tprintln(1) // 1\n}",
		caution: "括弧や文字列を閉じる前に終端になると構文エラーです。", reference: "https://go.dev/ref/spec#Source_code_representation",
	},
	token.COMMENT: {
		category: "字句 > コメント", title: "コメント",
		description: "//は行末まで、/* */は囲んだ部分を説明文にします。通常はプログラムの実行には影響しません。",
		syntax:      "// 説明 または /* 説明 */", example: "// 数を表示する\nn := 2 /* 初期値 */\nprintln(n) // 2",
		caution: "ブロックコメントは入れ子にできません。改行を含むコメントはセミコロン挿入にも影響します。", reference: "https://go.dev/ref/spec#Comments",
	},
	token.IDENT: {
		category: "名前 > 識別子", title: "名前とスコープ",
		description: "変数・関数・型などの名前です。どの宣言を指すかはスコープで決まり、内側の同名宣言が外側を隠す場合があります。",
		syntax:      "文字または_で始め、文字・数字・_を続ける", example: "value := 1\nprintln(value) // 1",
		caution: "先頭がUnicodeの大文字である名前は、所定の宣言位置でパッケージ外に公開されます。予約語は名前にできません。", reference: "https://go.dev/ref/spec#Declarations_and_scope",
	},
	token.INT: {
		category: "リテラル > 整数", title: "整数リテラル",
		description: "整数リテラルは整数の定数を記述します。0bは2進、0oは8進、0xは16進で、_で桁を区切れます。",
		syntax:      "123 / 0b101 / 0o77 / 0xff / 1_000", example: "n := 0x10\nprintln(n) // 16",
		caution: "先頭の0だけでも8進になります。型付きの値にするときは、その型に収まる必要があります。", reference: "https://go.dev/ref/spec#Integer_literals",
	},
	token.FLOAT: {
		category: "リテラル > 浮動小数点", title: "浮動小数点リテラル",
		description: "小数点や指数で実数の定数を表します。10進のe指数のほか、16進表記には2の累乗を示すp指数を使います。",
		syntax:      "1.5 / 1e3 / 0x1p2", example: "x := 1.5\nprintln(x == 3.0/2.0) // true",
		caution: "float32やfloat64に格納すると丸めが生じるため、小数の計算が数学上の値と一致するとは限りません。", reference: "https://go.dev/ref/spec#Floating-point_literals",
	},
	token.IMAG: {
		category: "リテラル > 虚数", title: "虚数リテラル",
		description: "数値にiを付けて虚部だけを持つ定数を記述します。実数と加算すると複素数を表せます。",
		syntax:      "2i / 1 + 2i", example: "z := 1 + 2i\nprintln(real(z) == 1, imag(z) == 2) // true true",
		caution: "iという変数との乗算ではありません。複素数に大小比較は使えません。", reference: "https://go.dev/ref/spec#Imaginary_literals",
	},
	token.CHAR: {
		category: "リテラル > rune", title: "runeリテラル",
		description: "単一引用符でUnicodeコードポイント1個を表す整数の定数です。型が指定されない変数への代入では通常runeになります。",
		syntax:      "'あ' / '\\n' / '\\u3042'", example: "r := 'あ'\nprintln(r == 0x3042) // true",
		caution: "文字列とは違い、複数のコードポイントは入れられません。見た目の1文字が複数コードポイントの場合もあります。", reference: "https://go.dev/ref/spec#Rune_literals",
	},
	token.STRING: {
		category: "リテラル > 文字列", title: "文字列リテラル",
		description: "文字列リテラルはバイト列の定数です。二重引用符はエスケープを解釈し、バッククォートは改行やバックスラッシュをほぼそのまま保持します。",
		syntax:      "\"文字列\" または `文字列`", example: "s := \"Go\\n\"\nprintln(len(s)) // 3",
		caution: "文字列は変更できません。lenは文字数ではなくバイト数で、raw文字列中のCRは値から除かれます。", reference: "https://go.dev/ref/spec#String_literals",
	},
	token.ADD: {
		category: "式 > 加算・連結", title: "+：加算と文字列連結",
		description: "数値同士なら加算、文字列同士なら連結です。単項の+は数値の符号を変えずに返します。",
		syntax:      "a + b / +x", example: "println(2 + 3) // 5\nprintln(\"Go\" + \"pher\") // Gopher",
		caution: "文字列と整数は直接連結できません。整数の型と範囲にも注意してください。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.SUB: {
		category: "式 > 減算", title: "-：減算と符号反転",
		description: "二項では左から右を引き、単項では数値の符号を反転します。",
		syntax:      "a - b / -x", example: "println(5 - 2) // 3\nprintln(-3) // -3",
		caution: "符号付き整数の最小値などは反転しても型の範囲を超え、数学上の結果にはなりません。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.MUL: {
		category: "記号 > アスタリスク", title: "*：乗算・ポインタ",
		description: "二項式では乗算、型ではポインタ型、単項式ではポインタの参照解除を表します。位置によって意味が異なります。",
		syntax:      "a * b / *T / *p", example: "n := 3\np := &n\nprintln(2 * *p) // 6",
		caution: "nilポインタの参照解除はパニックになります。型の*T自体は値の読み取りではありません。", reference: "https://go.dev/ref/spec#Address_operators",
	},
	token.QUO: {
		category: "式 > 除算", title: "/：除算",
		description: "数値を除算します。整数同士の商は小数部分をゼロ方向へ切り捨てます。",
		syntax:      "a / b", example: "println(7 / 2) // 3\nprintln(-7 / 2) // -3",
		caution: "整数のゼロ除算は実行時パニック、定数のゼロ除算はコンパイルエラーです。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.REM: {
		category: "式 > 剰余", title: "%：整数の余り",
		description: "整数除算の余りを返します。ゼロでなければ余りの符号は割られる側と同じです。",
		syntax:      "a % b", example: "println(7 % 3) // 1\nprintln(-7 % 3) // -1",
		caution: "浮動小数点には使えません。除数がゼロなら整数除算と同様に失敗します。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.AND: {
		category: "記号 > アンパサンド", title: "&：ビットAND・アドレス",
		description: "二項式では整数の両方で1のビットを残し、単項式では変数などのアドレスを取得します。",
		syntax:      "a & b / &x", example: "n := 6 & 3\np := &n\nprintln(*p) // 2",
		caution: "&は論理ANDの&&とは別です。mapの要素のアドレスは取得できません。", reference: "https://go.dev/ref/spec#Address_operators",
	},
	token.OR: {
		category: "式 > ビットOR", title: "|：ビットOR・型集合の和",
		description: "整数の少なくとも片側で1のビットを立てます。型制約内では型集合の和を表します。",
		syntax:      "a | b / interface { ~int | ~string }", example: "bits := 4 | 1\nprintln(bits) // 5",
		caution: "論理ORは||です。型制約の複数項の和には型集合の重なりなどの制限があります。", reference: "https://go.dev/ref/spec#General_interfaces",
	},
	token.XOR: {
		category: "式 > ビットXOR", title: "^：排他的OR・反転",
		description: "二項ではビットが異なる位置を1にし、単項では各ビットを反転します。",
		syntax:      "a ^ b / ^x", example: "println(6 ^ 3) // 5\nprintln(^uint8(0)) // 255",
		caution: "累乗の記号ではありません。単項反転の範囲はオペランドの型に依存します。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.SHL: {
		category: "式 > ビットシフト", title: "<<：左シフト",
		description: "整数のビットを指定数だけ左へ移します。型付き整数では型の幅を超える上位ビットが失われます。",
		syntax:      "x << n", example: "n := 1 << 3\nprintln(n) // 8",
		caution: "負のシフト数は許されません。右辺は整数型か非負整数で表現できる型なし定数にします。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.SHR: {
		category: "式 > ビットシフト", title: ">>：右シフト",
		description: "整数のビットを指定数だけ右へ移します。符号付き整数は符号を維持し、符号なし整数は上位を0で埋めます。",
		syntax:      "x >> n", example: "println(8 >> 2) // 2\nprintln(-8 >> 1) // -4",
		caution: "負のシフト数は実行時パニックまたは定数式のエラーです。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.AND_NOT: {
		category: "式 > ビットクリア", title: "&^：ビットを落とす",
		description: "右側が1の位置を左側から取り除き、それ以外の左側のビットを残します。",
		syntax:      "x &^ mask", example: "bits := 7 &^ 2\nprintln(bits) // 5",
		caution: "整数専用です。論理否定と論理ANDを組み合わせた演算ではありません。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	token.LAND: {
		category: "式 > 論理", title: "&&：論理AND",
		description: "両側の真偽値が真のとき真です。左側が偽なら右側を評価しない短絡評価を行います。",
		syntax:      "条件1 && 条件2", example: "p := new(int)\nprintln(p != nil && *p == 0) // true",
		caution: "オペランドはboolである必要があります。整数を暗黙に真偽値には変換しません。", reference: "https://go.dev/ref/spec#Logical_operators",
	},
	token.LOR: {
		category: "式 > 論理", title: "||：論理OR",
		description: "少なくとも一方の真偽値が真なら真です。左側が真なら右側を評価しません。",
		syntax:      "条件1 || 条件2", example: "n := 3\nprintln(n == 0 || n == 3) // true",
		caution: "&&の方が優先順位が高いため、複雑な条件は括弧で意図を明確にします。", reference: "https://go.dev/ref/spec#Logical_operators",
	},
	token.NOT: {
		category: "式 > 論理", title: "!：論理否定",
		description: "真偽値を反転します。trueをfalseに、falseをtrueにします。",
		syntax:      "!条件", example: "ready := false\nprintln(!ready) // true",
		caution: "ビット反転には^を使います。数値に!は使えません。", reference: "https://go.dev/ref/spec#Logical_operators",
	},
	token.EQL: {
		category: "式 > 比較", title: "==：等値比較",
		description: "比較可能な2つの値が等しいかを調べ、真偽値を返します。代入ではありません。",
		syntax:      "a == b", example: "println(3 == 3) // true\nprintln(\"Go\" == \"go\") // false",
		caution: "slice・map・関数はnilとの比較以外はできません。interfaceの動的な値が比較不能ならパニックになる場合があります。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.NEQ: {
		category: "式 > 比較", title: "!=：不等比較",
		description: "比較可能な2つの値が異なるかを調べ、真偽値を返します。nilとの比較にも使います。",
		syntax:      "a != b", example: "var p *int\nprintln(p != nil) // false",
		caution: "型付きnilポインタを格納したinterfaceはnilではありません。interfaceの型と動的な値を区別します。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.LSS: {
		category: "式 > 比較", title: "<：より小さい",
		description: "整数・浮動小数点・文字列など順序を持つ値について、左が右より小さいか調べます。",
		syntax:      "a < b", example: "println(2 < 3) // true\nprintln(\"a\" < \"b\") // true",
		caution: "文字列はバイト単位の辞書順です。boolや複素数には大小比較を使えません。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.LEQ: {
		category: "式 > 比較", title: "<=：以下",
		description: "順序を持つ値について、左が右より小さいか等しいとき真を返します。",
		syntax:      "a <= b", example: "n := 3\nprintln(n <= 3) // true",
		caution: "NaNを含む浮動小数点の大小比較は偽になります。境界値の含め方を確認します。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.GTR: {
		category: "式 > 比較", title: ">：より大きい",
		description: "順序を持つ値について、左が右より大きいとき真を返します。",
		syntax:      "a > b", example: "n := 3\nprintln(n > 0) // true",
		caution: "文字列の比較は言語別の照合順序や文字数の比較ではありません。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.GEQ: {
		category: "式 > 比較", title: ">=：以上",
		description: "順序を持つ値について、左が右より大きいか等しいとき真を返します。",
		syntax:      "a >= b", example: "n := 3\nprintln(n >= 3) // true",
		caution: "a >= b >= cのような連鎖比較はできません。&&で2つの比較をつなぎます。", reference: "https://go.dev/ref/spec#Comparison_operators",
	},
	token.ASSIGN: {
		category: "文 > 代入", title: "=：代入",
		description: "変数や要素に値を格納します。複数代入では右辺を評価してから左から順に代入します。",
		syntax:      "x = value / a, b = b, a", example: "a, b := 1, 2\na, b = b, a\nprintln(a, b) // 2 1",
		caution: "比較は==です。型の別名宣言やconstの=は、実行時の変数代入とは異なります。", reference: "https://go.dev/ref/spec#Assignment_statements",
	},
	token.DEFINE: {
		category: "宣言 > 短い変数宣言", title: ":=：宣言と初期化",
		description: "右辺から型を推論して変数を宣言します。同じスコープに少なくとも1つ、空白識別子以外の新しい変数が必要です。",
		syntax:      "name := value / a, b := x, y", example: "n := 1\nn, m := 2, 3\nprintln(n, m) // 2 3",
		caution: "関数内でのみ使えます。内側のブロックで同じ名前を宣言すると、外側の変数を更新せず隠します。", reference: "https://go.dev/ref/spec#Short_variable_declarations",
	},
	token.INC: {
		category: "文 > 増減", title: "++：1増やす",
		description: "数値の変数や要素を1増やす文です。式として値を返しません。",
		syntax:      "x++", example: "n := 1\nn++\nprintln(n) // 2",
		caution: "++xやy := x++は書けません。並行アクセスを安全にする原子的操作でもありません。", reference: "https://go.dev/ref/spec#IncDec_statements",
	},
	token.DEC: {
		category: "文 > 増減", title: "--：1減らす",
		description: "数値の変数や要素を1減らす文です。演算結果を式として使用することはできません。",
		syntax:      "x--", example: "n := 2\nn--\nprintln(n) // 1",
		caution: "前置の--xはありません。符号なし整数では0から減らすと型の最大値になります。", reference: "https://go.dev/ref/spec#IncDec_statements",
	},
	token.ARROW: {
		category: "並行 > チャネル操作", title: "<-：送信・受信・方向",
		description: "ch <- vは送信、<-chは受信です。型のchan<- Tと<-chan Tは許可する操作の方向を制限します。",
		syntax:      "ch <- value / <-ch / chan<- T / <-chan T", example: "ch := make(chan int, 1)\nch <- 7\nprintln(<-ch) // 7",
		caution: "nilチャネルの送受信は永久に待機します。閉じたチャネルへの送信はパニックです。", reference: "https://go.dev/ref/spec#Channel_types",
	},
	token.ELLIPSIS: {
		category: "記号 > 省略記号", title: "...：可変長・展開・配列長",
		description: "引数宣言の可変長、呼出し時のスライス展開、配列リテラルの長さ推論に使います。位置ごとに別の役割があります。",
		syntax:      "func f(xs ...T) / f(xs...) / [...]T{...}", example: "xs := [...]int{1, 2}\nprintln(len(xs)) // 2",
		caution: "単なる省略可能な任意のコードを表す記号ではありません。可変長パラメータは最後に置きます。", reference: "https://go.dev/ref/spec#Passing_arguments_to_..._parameters",
	},
	token.TILDE: {
		category: "型 > 制約・基底型", title: "~：基底型による型集合",
		description: "型制約の~Tは基底型がTである型の集合です。例えば~intにはintを基底型とする定義型も含まれます。",
		syntax:      "type Integer interface { ~int | ~int64 }", example: "type Count int\nfunc twice[T ~int](v T) T {\n\treturn v + v // twice(Count(3)) は6\n}",
		caution: "~TのTは自身を基底型に持ち、interfaceであってはなりません。この種のinterfaceは変数の型には使えません。", reference: "https://go.dev/ref/spec#General_interfaces",
	},
	token.LPAREN: {
		category: "記号 > 丸括弧", title: "(：引数・グループの開始",
		description: "関数のパラメータや引数、変換、式のグループを開始します。宣言のグループ化にも使います。",
		syntax:      "f(x) / (a + b) / var (...)", example: "n := (2 + 3) * 4\nprintln(n) // 20",
		caution: "ifやforの条件を括弧で囲む必要はありません。閉じ括弧との対応を確認します。", reference: "https://go.dev/ref/spec#Primary_expressions",
	},
	token.RPAREN: {
		category: "記号 > 丸括弧", title: ")：引数・グループの終わり",
		description: "丸括弧で始めた引数リスト、パラメータ、式や宣言のまとまりを閉じます。",
		syntax:      "f(a, b) / (expression)", example: "println((2 + 3) * 4)\n// 20",
		caution: "行末の)はセミコロンの自動挿入対象です。複数行の引数では最後の要素にカンマを付けます。", reference: "https://go.dev/ref/spec#Semicolons",
	},
	token.LBRACK: {
		category: "記号 > 角括弧", title: "[：型・添字の開始",
		description: "配列やスライスの型、添字、スライス式、型パラメータや型引数を開始します。",
		syntax:      "[N]T / []T / x[i] / x[lo:hi] / F[T]", example: "xs := []int{4, 5}\nprintln(xs[0]) // 4",
		caution: "[]Tと[N]Tは異なる型です。[]は関数呼出しの引数には使いません。", reference: "https://go.dev/ref/spec#Array_types",
	},
	token.RBRACK: {
		category: "記号 > 角括弧", title: "]：型・添字の終わり",
		description: "添字や範囲、配列長、型引数などの角括弧を閉じます。",
		syntax:      "x[i] / x[lo:hi] / []T", example: "xs := []int{4, 5}\nprintln(len(xs[0:1])) // 1",
		caution: "添字の上限は含まず、通常の添字は0以上len未満です。", reference: "https://go.dev/ref/spec#Index_expressions",
	},
	token.LBRACE: {
		category: "記号 > 波括弧", title: "{：ブロック・要素の開始",
		description: "処理ブロック、構造体やinterfaceの型本体、複合リテラルの要素を開始します。",
		syntax:      "if condition { ... } / T{...}", example: "if true {\n\tprintln(1) // 1\n}",
		caution: "ifやforの開始波括弧を次の行に置くと、セミコロン挿入によってエラーになる場合があります。", reference: "https://go.dev/ref/spec#Blocks",
	},
	token.RBRACE: {
		category: "記号 > 波括弧", title: "}：ブロック・要素の終わり",
		description: "ブロック、型本体、複合リテラルを閉じます。ブロックの終了で局所変数のスコープも終わります。",
		syntax:      "{ statements } / T{elements}", example: "xs := []int{\n\t1, 2,\n}\nprintln(len(xs)) // 2",
		caution: "複数行の複合リテラルでは最終要素にもカンマが必要です。elseは直前の}と同じ行に置きます。", reference: "https://go.dev/ref/spec#Blocks",
	},
	token.COMMA: {
		category: "記号 > 区切り", title: ",：項目の区切り",
		description: "引数・要素・複数の名前や値などを区切ります。複数代入でも対応する位置を表します。",
		syntax:      "a, b := x, y / f(a, b)", example: "a, b := 1, 2\nprintln(a, b) // 1 2",
		caution: "カンマは式を順に実行する演算子ではありません。改行前の末尾カンマはセミコロンの挿入を防ぎます。", reference: "https://go.dev/ref/spec#Expressions",
	},
	token.PERIOD: {
		category: "式 > 選択", title: ".：セレクタ",
		description: "パッケージの公開名、構造体のフィールド、メソッドを選びます。x.(T)では型アサーションの一部です。",
		syntax:      "pkg.Name / value.Field / value.Method", example: "v := struct{ N int }{N: 3}\nprintln(v.N) // 3",
		caution: "他のパッケージの非公開名は選べません。埋め込みによる同順位の候補が複数ある場合もエラーです。", reference: "https://go.dev/ref/spec#Selectors",
	},
	token.COLON: {
		category: "記号 > 区切り", title: ":：キー・範囲・分岐",
		description: "複合リテラルのキーと値、スライスの境界、caseやラベルの見出しを区切ります。",
		syntax:      "key: value / x[lo:hi] / case value:", example: "m := map[string]int{\"Go\": 3}\nprintln(m[\"Go\"]) // 3",
		caution: "型注釈の記号ではありません。変数はvar n intのように名前の後に型を書きます。", reference: "https://go.dev/ref/spec#Composite_literals",
	},
	token.SEMICOLON: {
		category: "字句 > 文の区切り", title: ";：セミコロン",
		description: "文を区切ります。多くは行末で自動挿入されるため省略できますが、for節などでは明示して区切ります。",
		syntax:      "statement; statement / for init; condition; post { ... }", example: "for i := 0; i < 2; i++ {\n\tprintln(i) // 順に0、1\n}",
		caution: "改行なら必ず挿入されるわけではなく、行末のトークンによって決まります。", reference: "https://go.dev/ref/spec#Semicolons",
	},
	token.BREAK: {
		category: "制御 > 脱出", title: "break：繰返し・分岐から出る",
		description: "最も内側のfor・switch・selectを終了します。ラベル付きなら対応する外側の構文から出られます。",
		syntax:      "break / break Label", example: "for i := 0; ; i++ {\n\tif i == 2 { break }\n\tprintln(i) // 順に0、1\n}",
		caution: "switch内のbreakだけでは、その外側のforは終了しません。", reference: "https://go.dev/ref/spec#Break_statements",
	},
	token.CASE: {
		category: "制御 > 分岐候補", title: "case：分岐の候補",
		description: "switchでは値・型の候補、selectでは送受信操作の候補を指定します。構文の親によって意味が変わります。",
		syntax:      "case value: / case Type: / case v := <-ch:", example: "switch 2 {\ncase 1, 2:\n\tprintln(\"small\") // small\n}",
		caution: "通常のswitchは一致した節だけを実行し、自動で次のcaseへ進みません。", reference: "https://go.dev/ref/spec#Switch_statements",
	},
	token.CHAN: {
		category: "型 > チャネル", title: "chan：チャネル型",
		description: "同じ要素型の値をゴルーチン間で受け渡す型です。makeで作り、容量0なら送信と受信が互いに揃うまで待機します。",
		syntax:      "chan T / chan<- T / <-chan T", example: "ch := make(chan int, 1)\nch <- 3\nprintln(<-ch) // 3",
		caution: "ゼロ値はnilで、送受信は永久に待機します。容量は通信の完了待ちや排他制御そのものの代替ではありません。", reference: "https://go.dev/ref/spec#Channel_types",
	},
	token.CONST: {
		category: "宣言 > 定数", title: "const：定数宣言",
		description: "constはコンパイル時に決まる真偽値・数値・文字列の定数を宣言します。型を指定しない定数は利用する文脈で型が決まります。",
		syntax:      "const Name = constantExpression", example: "const size = 3\nprintln(size * 2) // 6",
		caution: "slice・map・関数呼出しの任意の結果を定数にはできません。型付き定数はその型の範囲に収めます。", reference: "https://go.dev/ref/spec#Constant_declarations",
	},
	token.CONTINUE: {
		category: "制御 > 繰返し", title: "continue：次の反復へ",
		description: "forの残りを飛ばして次の反復へ進みます。3要素のfor節では更新文を実行してから条件を再確認します。",
		syntax:      "continue / continue Label", example: "for i := 0; i < 3; i++ {\n\tif i == 1 { continue }\n\tprintln(i) // 順に0、2\n}",
		caution: "ラベルの対象もforでなければなりません。条件だけのループでは更新を飛ばして無限ループにしないよう注意します。", reference: "https://go.dev/ref/spec#Continue_statements",
	},
	token.DEFAULT: {
		category: "制御 > 既定分岐", title: "default：他に候補がないとき",
		description: "switchではどのcaseにも一致しないとき、selectでは直ちに実行できる送受信がないときに実行します。",
		syntax:      "default: statements", example: "switch 3 {\ncase 1: println(1)\ndefault: println(\"other\") // other\n}",
		caution: "selectにdefaultを付けると待機しません。ループ内では空回りしてCPUを消費することがあります。", reference: "https://go.dev/ref/spec#Select_statements",
	},
	token.DEFER: {
		category: "制御 > 終了時処理", title: "defer：呼出しを後に回す",
		description: "現在の関数が戻る際に呼出しを実行します。関数と引数は登録時に評価され、複数のdeferは登録と逆順に実行されます。",
		syntax:      "defer function(arguments)", example: "func cleanup() {\n\tdefer println(1)\n\tprintln(2) // cleanup() は2、1の順に表示\n}",
		caution: "ブロックやループの終了時ではなく関数終了時です。ループで資源の解放を登録すると保持期間が延びます。", reference: "https://go.dev/ref/spec#Defer_statements",
	},
	token.ELSE: {
		category: "制御 > 条件分岐", title: "else：条件が偽のとき",
		description: "ifの条件が偽だった場合に実行するブロックや次のifを指定します。ifの初期化変数はelseでも使えます。",
		syntax:      "if condition { ... } else if condition { ... } else { ... }", example: "if n := 2; n < 0 {\n\tprintln(\"negative\")\n} else {\n\tprintln(n) // 2\n}",
		caution: "elseは直前の閉じ波括弧と同じ行に書きます。改行するとセミコロンが挿入されます。", reference: "https://go.dev/ref/spec#If_statements",
	},
	token.FALLTHROUGH: {
		category: "制御 > 分岐の継続", title: "fallthrough：次の節へ進む",
		description: "式switchの次のcaseの条件を確認せず、その本体へ進みます。通常のcaseの自動終了を明示的に変えます。",
		syntax:      "case value: statements; fallthrough", example: "switch 1 {\ncase 1:\n\tprintln(1) // 1\n\tfallthrough\ncase 2:\n\tprintln(2) // 2\n}",
		caution: "型switchでは使えません。最後以外の節の最後の空でない文に置く必要があります。", reference: "https://go.dev/ref/spec#Fallthrough_statements",
	},
	token.FOR: {
		category: "制御 > 繰返し", title: "for：繰返し",
		description: "Goのループはforで書きます。初期化・条件・更新、条件だけ、無限ループ、rangeによる列挙を選べます。",
		syntax:      "for init; condition; post { ... } / for condition { ... } / for { ... }", example: "for i := 0; i < 3; i++ {\n\tprintln(i) // 順に0、1、2\n}",
		caution: "Go 1.22以降、ループで宣言した変数は反復ごとに新しくなります。外で宣言した変数への代入とは区別します。", reference: "https://go.dev/ref/spec#For_statements",
	},
	token.FUNC: {
		category: "宣言 > 関数", title: "func：関数の宣言・型・リテラル",
		description: "引数と戻り値を持つ処理を表します。名前付き関数、レシーバ付きメソッド、無名関数、関数型を記述します。",
		syntax:      "func name(parameters) results { body }", example: "func add(a, b int) int {\n\treturn a + b // add(2, 3) は5\n}",
		caution: "名前付き関数宣言は別の関数の内部には置けません。その場合は関数リテラルを変数に代入します。", reference: "https://go.dev/ref/spec#Function_declarations",
	},
	token.GO: {
		category: "並行 > ゴルーチン", title: "go：並行する呼出し",
		description: "関数呼出しを独立したゴルーチンで実行します。引数は呼出し元で評価し、完了を待たず次の文へ進みます。",
		syntax:      "go function(arguments)", example: "done := make(chan int)\ngo func() { done <- 7 }()\nprintln(<-done) // 7",
		caution: "mainの終了時に他のゴルーチンを自動で待ちません。共有変数への並行アクセスには同期が必要です。", reference: "https://go.dev/ref/spec#Go_statements",
	},
	token.GOTO: {
		category: "制御 > ジャンプ", title: "goto：ラベルへ移動",
		description: "同じ関数内のラベルへ制御を移します。ラベルはブロックスコープの変数名とは別に扱われます。",
		syntax:      "goto Label / Label: statement", example: "func jump() {\n\tgoto done\ndone:\n\tprintln(1) // jump() は1を表示\n}",
		caution: "変数宣言を飛び越えてスコープに入ったり、別のブロックの内部へ外側から飛び込んだりはできません。", reference: "https://go.dev/ref/spec#Goto_statements",
	},
	token.IF: {
		category: "制御 > 条件分岐", title: "if：条件による分岐",
		description: "条件の真偽値が真ならブロックを実行します。条件の前に短い初期化文を置けます。",
		syntax:      "if [simpleStatement;] condition { ... }", example: "if n := 3; n > 0 {\n\tprintln(n) // 3\n}",
		caution: "条件にはboolが必要です。初期化文の変数のスコープはelseを含むif全体までです。", reference: "https://go.dev/ref/spec#If_statements",
	},
	token.IMPORT: {
		category: "パッケージ > インポート", title: "import：他のパッケージを使う",
		description: "インポートパスでパッケージを指定します。取り込んだ名前を使って公開された識別子へアクセスします。",
		syntax:      "import [alias] \"path\"", example: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Go\") // Go\n}",
		caution: "未使用のインポートはエラーです。副作用だけ必要なら_の別名を使います。インポート宣言は関数内に置けません。", reference: "https://go.dev/ref/spec#Import_declarations",
	},
	token.INTERFACE: {
		category: "型 > インターフェース", title: "interface：振る舞いと型集合",
		description: "メソッドの要件や型集合を記述します。通常のinterfaceの値は動的な型と値を保持し、実装の宣言を明示する必要はありません。",
		syntax:      "type I interface { Method() } / interface { ~int | ~string }", example: "var x interface{} = 3\nn, ok := x.(int)\nprintln(n, ok) // 3 true",
		caution: "型項や~や和集合を持つ非基本interfaceは型制約専用で、変数の型にはできません。型付きnilを入れたinterfaceはnilではありません。", reference: "https://go.dev/ref/spec#Interface_types",
	},
	token.MAP: {
		category: "型 > マップ", title: "map：キーと値の対応",
		description: "比較可能なキーから値を引く型です。未登録キーの読み取りは要素型のゼロ値となり、2値形式なら存在も確認できます。",
		syntax:      "map[K]V / make(map[K]V) / value, ok := m[key]", example: "var m map[string]int\nv, ok := m[\"x\"]\nprintln(v, ok, len(m)) // 0 false 0",
		caution: "nilのmapは読取り・len・deleteが可能ですが書込みはパニックです。反復順序は未規定で、並行する読書きには同期が必要です。", reference: "https://go.dev/ref/spec#Map_types",
	},
	token.PACKAGE: {
		category: "パッケージ > 宣言", title: "package：ファイルの所属",
		description: "ソースファイルが属するパッケージ名を宣言します。実行プログラムの入口はmainパッケージのmain関数です。",
		syntax:      "package name", example: "package main\n\nfunc main() {\n\tprintln(1) // 1\n}",
		caution: "パッケージ名はインポートパスそのものではありません。package節はファイルの最初に置きます。", reference: "https://go.dev/ref/spec#Package_clause",
	},
	token.RANGE: {
		category: "制御 > 列挙", title: "range：要素を列挙する",
		description: "配列・slice・文字列・map・チャネルなどを列挙します。文字列はバイト位置とrune、チャネルは受信値を返します。Go 1.22で整数、Go 1.23でイテレータ関数にも対応しました。",
		syntax:      "for key, value := range expression { ... }", example: "for i, v := range []int{4, 5} {\n\tprintln(i, v) // 順に0 4、1 5\n}",
		caution: "mapの順序は未規定です。nilのslice・mapは0回、nilチャネルは待機します。:=の変数はGo 1.22以降反復ごとに新しくなります。", reference: "https://go.dev/ref/spec#For_range",
	},
	token.RETURN: {
		category: "制御 > 関数終了", title: "return：戻り値と終了",
		description: "関数を終了し、宣言された結果を呼出し元に返します。終了前に登録済みのdeferが実行されます。",
		syntax:      "return / return result1, result2", example: "func double(n int) int {\n\treturn n * 2 // double(3) は6\n}",
		caution: "戻り値の個数と型を宣言に合わせます。名前付き結果の裸のreturnは、同名変数による隠蔽にも注意が必要です。", reference: "https://go.dev/ref/spec#Return_statements",
	},
	token.SELECT: {
		category: "並行 > 通信の選択", title: "select：進める通信を選ぶ",
		description: "複数のチャネル操作から実行できるものを選びます。複数可能なら一様な擬似ランダム選択、どれも不可ならdefaultか待機です。",
		syntax:      "select { case v := <-ch: ...; case out <- v: ...; default: ... }", example: "ch := make(chan int, 1)\nch <- 3\nselect {\ncase v := <-ch: println(v) // 3\ndefault: println(\"empty\")\n}",
		caution: "caseの記述順は優先順位ではありません。nilチャネルのcaseは進まず、空のselectは永久に待機します。", reference: "https://go.dev/ref/spec#Select_statements",
	},
	token.STRUCT: {
		category: "型 > 構造体", title: "struct：フィールドをまとめる",
		description: "名前と型を持つ複数のフィールドを1つの値にまとめます。フィールドのゼロ値を組み合わせたものが構造体のゼロ値です。",
		syntax:      "type Name struct { Field Type }", example: "type User struct { Name string; Age int }\nu := User{Name: \"Go\"}\nprintln(u.Name, u.Age) // Go 0",
		caution: "代入は構造体の値をコピーしますが、sliceやmapなどのフィールドの参照先まで複製するわけではありません。", reference: "https://go.dev/ref/spec#Struct_types",
	},
	token.SWITCH: {
		category: "制御 > 多分岐", title: "switch：式や型で分岐",
		description: "式switchは値や条件、型switchはinterfaceの動的な型によって分岐します。通常は選ばれた節だけを実行します。",
		syntax:      "switch [init;] value { case ...: ... } / switch x.(type) { ... }", example: "switch n := 2; n {\ncase 2: println(\"two\") // two\ndefault: println(\"other\")\n}",
		caution: "式を省くとswitch trueと同じです。型switchではfallthroughは使えません。", reference: "https://go.dev/ref/spec#Switch_statements",
	},
	token.TYPE: {
		category: "宣言 > 型", title: "type：型の定義と別名",
		description: "type Name Tは独立した定義型、type Name = Tは同一の型への別名を宣言します。基底型が同じでも定義型は別の型です。",
		syntax:      "type Name T / type Name = T", example: "type Count int\nvar n Count = 3\nprintln(int(n)) // 3",
		caution: "別の定義型との代入では明示的な変換が必要になることがあります。型の別名は新しい型を作りません。", reference: "https://go.dev/ref/spec#Type_declarations",
	},
	token.VAR: {
		category: "宣言 > 変数", title: "var：変数宣言",
		description: "変数を宣言します。型か初期値を指定し、初期値がなければ型のゼロ値になります。関数外でも使えます。",
		syntax:      "var name Type / var name = expression", example: "var n int\nvar ready bool\nprintln(n, ready) // 0 false",
		caution: "nilを単独の初期値にして型を推論することはできません。未使用の局所変数はエラーです。", reference: "https://go.dev/ref/spec#Variable_declarations",
	},
}

var syntaxTopics = map[string]syntaxHelp{
	"pointer_type": {
		category: "型 > ポインタ", title: "*T：ポインタ型",
		description: "Tの変数を指す値の型です。ポインタをコピーすると同じ変数を指し、参照解除して更新すると元の変数に反映されます。",
		syntax:      "var p *T", example: "n := 3\nvar p *int = &n\nprintln(*p) // 3",
		caution: "ゼロ値はnilです。Goには一般のポインタ算術はありません。", reference: "https://go.dev/ref/spec#Pointer_types",
	},
	"dereference": {
		category: "式 > 参照解除", title: "*p：ポインタの参照先",
		description: "ポインタが指す変数を取り出します。読み取るだけでなく、代入の左辺として元の変数を書き換えられます。",
		syntax:      "*p / *p = value", example: "n := 1\np := &n\n*p = 4\nprintln(n) // 4",
		caution: "nilの参照解除は実行時パニックになります。", reference: "https://go.dev/ref/spec#Address_operators",
	},
	"address_of": {
		category: "式 > アドレス取得", title: "&x：変数のアドレス",
		description: "アドレス可能な変数などへのポインタを作ります。&T{...}の形で複合リテラルを指すこともできます。",
		syntax:      "&variable / &T{elements}", example: "n := 2\np := &n\n*p = 5\nprintln(n) // 5",
		caution: "mapの要素や通常の関数の戻り値に直接&は付けられません。", reference: "https://go.dev/ref/spec#Address_operators",
	},
	"bitwise_and": {
		category: "式 > ビットAND", title: "a & b：共通ビット",
		description: "2つの整数の同じ位置のビットが両方1のときだけ、その位置を1にします。マスクで特定のビットを抽出できます。",
		syntax:      "integer & mask", example: "n := 6 & 3\nprintln(n) // 2",
		caution: "boolに使う論理ANDは&&です。単項の&はアドレス取得です。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	"multiply": {
		category: "式 > 乗算", title: "a * b：掛け算",
		description: "数値同士を掛けます。単項の参照解除やポインタ型の*とは異なり、左右にオペランドを取ります。",
		syntax:      "number * number", example: "n := 3 * 4\nprintln(n) // 12",
		caution: "整数演算は型の幅を超えるとオーバーフローします。文字列を回数分繰り返す演算には使えません。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	"unary_plus": {
		category: "式 > 単項正符号", title: "+x：数値をそのまま返す",
		description: "数値の値と符号をそのまま保つ単項演算です。加算と異なりオペランドは1つです。",
		syntax:      "+number", example: "n := -3\nprintln(+n) // -3",
		caution: "負数を正数にする絶対値演算ではありません。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	"unary_minus": {
		category: "式 > 符号反転", title: "-x：数値の符号を反転",
		description: "数値の加法逆元を計算します。二項の減算と異なり、オペランドは1つです。",
		syntax:      "-number", example: "n := 3\nprintln(-n) // -3",
		caution: "符号付き整数の最小値ではオーバーフローが起こります。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	"bitwise_not": {
		category: "式 > ビット反転", title: "^x：ビットを反転",
		description: "整数の各ビットを反転します。符号なしなら型の幅全体で0と1を入れ替えます。",
		syntax:      "^integer", example: "var n uint8 = 15\nprintln(^n) // 240",
		caution: "真偽値の否定には!を使います。型なし定数の^0は-1になります。", reference: "https://go.dev/ref/spec#Arithmetic_operators",
	},
	"receive": {
		category: "並行 > 受信", title: "<-ch：チャネルから受信",
		description: "チャネルから値を1つ受け取ります。2値形式のokは、送信された値を受け取れたとき真です。",
		syntax:      "value := <-ch / value, ok := <-ch", example: "ch := make(chan int, 1)\nch <- 7\nclose(ch)\na, ok := <-ch\nb, more := <-ch\nprintln(a, ok, b, more) // 7 true 0 false",
		caution: "閉じてもバッファの値は先に受信します。空になった閉鎖済みチャネルはゼロ値とfalse、nilチャネルは永久待機です。", reference: "https://go.dev/ref/spec#Receive_operator",
	},
	"send": {
		category: "並行 > 送信", title: "ch <- value：チャネルへ送信",
		description: "チャネルの要素型に合う値を送る文です。バッファに空きがあるか、受信相手が用意できるまで待機します。",
		syntax:      "channel <- value", example: "ch := make(chan int, 1)\nch <- 8\nprintln(<-ch) // 8",
		caution: "閉じたチャネルへの送信はパニック、nilへの送信は永久待機です。送信は値を返す式ではありません。", reference: "https://go.dev/ref/spec#Send_statements",
	},
	"chan_send": {
		category: "型 > 送信専用チャネル", title: "chan<- T：送信のみ許可",
		description: "その変数を通しては送信だけできるチャネル型です。双方向チャネルを関数へ渡す際、使える操作を制限できます。",
		syntax:      "chan<- T", example: "ch := make(chan int, 1)\nvar out chan<- int = ch\nout <- 2\nprintln(<-ch) // 2",
		caution: "送信専用でもcloseは可能です。方向はチャネルの複製を意味せず、同じチャネルを共有します。", reference: "https://go.dev/ref/spec#Channel_types",
	},
	"chan_receive": {
		category: "型 > 受信専用チャネル", title: "<-chan T：受信のみ許可",
		description: "その変数を通しては受信だけできるチャネル型です。受信側APIが誤って送信・closeしないよう型で制限します。",
		syntax:      "<-chan T", example: "ch := make(chan int, 1)\nch <- 2\nvar in <-chan int = ch\nprintln(<-in) // 2",
		caution: "受信専用のチャネルはcloseできません。nilなら受信は待機したままです。", reference: "https://go.dev/ref/spec#Channel_types",
	},
	"for_clause": {
		category: "制御 > 繰返し", title: "初期化・条件・更新のfor",
		description: "最初に初期化し、条件が真の間だけ本体を実行し、その後に更新文を実行します。",
		syntax:      "for init; condition; post { body }", example: "for i := 0; i < 3; i++ {\n\tprintln(i) // 順に0、1、2\n}",
		caution: "更新文に:=は使えません。Go 1.22以降、宣言した反復変数は反復ごとに別の変数になります。", reference: "https://go.dev/ref/spec#For_clause",
	},
	"for_condition": {
		category: "制御 > 繰返し", title: "条件だけのfor",
		description: "各反復の前にboolの条件を確認します。他言語のwhileに相当する書き方です。",
		syntax:      "for condition { body }", example: "n := 0\nfor n < 2 {\n\tprintln(n) // 順に0、1\n\tn++\n}",
		caution: "条件を変える処理がなければループが終わらないことがあります。", reference: "https://go.dev/ref/spec#For_statements",
	},
	"for_infinite": {
		category: "制御 > 繰返し", title: "条件を省略したfor",
		description: "条件を省くと常に真として繰り返します。breakやreturnなどで明示的に終了します。",
		syntax:      "for { body }", example: "for {\n\tprintln(1) // 1を1回表示\n\tbreak\n}",
		caution: "待機や終了条件のないループはCPUを消費します。", reference: "https://go.dev/ref/spec#For_statements",
	},
	"range_loop": {
		category: "制御 > 列挙", title: "rangeループ",
		description: "コレクションや受信値を列挙します。Go 1.22以降は整数nについて0からn未満、Go 1.23以降はyieldを受け取るイテレータ関数も使えます。",
		syntax:      "for index, value := range sequence { ... } / for i := range n { ... }", example: "for i := range 3 {\n\tprintln(i) // 順に0、1、2（Go 1.22以降）\n}",
		caution: "文字列はバイト位置とruneです。mapの順序は未規定、nilのmap・sliceは0回、nilチャネルは待機します。:=の変数もGo 1.22で反復ごとの変数に変わりました。", reference: "https://go.dev/ref/spec#For_range",
	},
	"expression_switch": {
		category: "制御 > 値による分岐", title: "式switch",
		description: "式を1度評価し、上からcaseを調べて最初に一致した節を実行します。式を省くとtrueとの比較になります。",
		syntax:      "switch [init;] expression { case values: ...; default: ... }", example: "n := 3\nswitch {\ncase n < 0: println(\"negative\")\ncase n > 0: println(\"positive\") // positive\ndefault: println(\"zero\")\n}",
		caution: "通常はbreakなしで節を終了します。複数条件が真なら先に書いたcaseが選ばれます。", reference: "https://go.dev/ref/spec#Expression_switches",
	},
	"type_switch": {
		category: "制御 > 型による分岐", title: "型switch",
		description: "interface値の動的な型で分岐します。単一の型を指定したcaseでは、宣言変数をその型で利用できます。",
		syntax:      "switch v := x.(type) { case T: ...; case nil: ... }", example: "var x any = 3\nswitch v := x.(type) {\ncase int: println(v + 1) // 4\ndefault: println(\"other\")\n}",
		caution: ".(type)はこの構文専用です。fallthroughは使えず、複数型のcaseでは変数の型は元のinterfaceの型です。", reference: "https://go.dev/ref/spec#Type_switches",
	},
	"select_case": {
		category: "並行 > 通信の分岐", title: "selectのcase",
		description: "送信または受信を候補にします。selectに入ると各通信のチャネルと送信値が1度評価され、実行可能な通信を選びます。",
		syntax:      "case ch <- value: / case value, ok := <-ch:", example: "ch := make(chan int, 1)\nch <- 9\nselect {\ncase v := <-ch: println(v) // 9\n}",
		caution: "受信の代入先はそのcaseが選ばれてから評価します。複数可能でもcaseの記述順で優先しません。", reference: "https://go.dev/ref/spec#Select_statements",
	},
	"switch_case": {
		category: "制御 > 分岐候補", title: "switchのcase",
		description: "式switchでは一致する値、型switchでは動的な型の候補を指定します。カンマで複数の候補を列挙できます。",
		syntax:      "case value1, value2: statements / case T: statements", example: "switch 2 {\ncase 1, 2:\n\tprintln(\"small\") // small\ndefault:\n\tprintln(\"other\")\n}",
		caution: "同じ定数のcaseや型switchで同一型のcaseは重複できません。", reference: "https://go.dev/ref/spec#Switch_statements",
	},
	"func_decl": {
		category: "宣言 > 関数", title: "名前付き関数宣言",
		description: "パッケージに関数名を宣言します。引数と結果の型が呼出し方を定め、複数の戻り値も宣言できます。",
		syntax:      "func Name(parameters) (results) { body }", example: "func add(a, b int) int {\n\treturn a + b // add(2, 3) は5\n}",
		caution: "名前付き関数は関数内で宣言できません。型パラメータを宣言する場合は関数名の後に置きます。", reference: "https://go.dev/ref/spec#Function_declarations",
	},
	"method_decl": {
		category: "宣言 > メソッド", title: "レシーバ付き関数宣言",
		description: "レシーバの型にメソッドを関連付けます。ポインタレシーバなら元の値を変更する処理を記述できます。",
		syntax:      "func (receiver T) Name(parameters) results { body }", example: "type Counter int\nfunc (c *Counter) Inc() {\n\t*c++ // cが指す値を1増やす\n}\nfunc counterExample() {\n\tvar c Counter\n\tc.Inc()\n\tprintln(c) // 1\n}",
		caution: "基底のレシーバ型は同じパッケージで定義します。Tと*Tのメソッド集合は異なります。", reference: "https://go.dev/ref/spec#Method_declarations",
	},
	"func_literal": {
		category: "式 > 無名関数", title: "関数リテラルとクロージャ",
		description: "名前のない関数の値を作ります。外側の変数を参照でき、捕捉した変数は関数と共有されます。",
		syntax:      "func(parameters) results { body }", example: "n := 1\nf := func() { n++ }\nf()\nprintln(n) // 2",
		caution: "外側の値の固定コピーを自動的に作るわけではありません。共有変数を並行更新する場合は同期が必要です。", reference: "https://go.dev/ref/spec#Function_literals",
	},
	"func_type": {
		category: "型 > 関数", title: "関数型",
		description: "パラメータと結果の型で関数値の型を表します。関数を変数や引数、戻り値として扱えます。",
		syntax:      "func(parameters) results", example: "var f func(int) int = func(n int) int { return n * 2 }\nprintln(f(3)) // 6",
		caution: "関数型のゼロ値はnilで、呼ぶとパニックです。関数値同士は比較できずnilとの比較のみ可能です。", reference: "https://go.dev/ref/spec#Function_types",
	},
	"slice_type": {
		category: "型 > スライス", title: "[]T：スライス型",
		description: "基底配列の一部分を指す、長さと容量を持つ型です。ゼロ値のnilスライスは長さも容量も0です。",
		syntax:      "[]T / make([]T, length, capacity)", example: "var xs []int\nprintln(xs == nil, len(xs), cap(xs)) // true 0 0\nxs = append(xs, 4)\nprintln(xs[0]) // 4",
		caution: "nilでもappendやrangeは可能ですが、要素がない状態の添字アクセスはパニックです。代入は要素を複製せず基底配列を共有します。", reference: "https://go.dev/ref/spec#Slice_types",
	},
	"array_type": {
		category: "型 > 配列", title: "[N]T：固定長配列",
		description: "要素型と長さで決まる型です。長さは非負の定数で、[2]intと[3]intは別の型です。",
		syntax:      "[constantLength]ElementType", example: "var a [2]int\na[0] = 3\nb := a\nb[0] = 9\nprintln(a[0], b[0]) // 3 9",
		caution: "配列の代入は配列の値をコピーします。[]Tというスライス型とは区別します。", reference: "https://go.dev/ref/spec#Array_types",
	},
	"type_arguments": {
		category: "型 > ジェネリクス", title: "型引数によるインスタンス化",
		description: "ジェネリックな型や関数の型パラメータに具体的な型を渡します。各型引数は対応する型制約を満たす必要があります。",
		syntax:      "Generic[T] / function[T1, T2](args)",
		example:     "type Box[T any] struct { Value T }\nb := Box[int]{Value: 3}\nprintln(b.Value) // 3",
		caution:     "型引数が1つの式f[T]は添字式と同じ構文です。型の位置など構文だけで確定できる場合を除き、両者の区別には名前と型の解決が必要です。",
		reference:   "https://go.dev/ref/spec#Instantiations",
	},
	"index": {
		category: "式 > 添字・型引数候補", title: "x[i]：添字式と型引数の区別",
		description: "配列・スライス・文字列では0始まりの添字、mapではキーで要素を選びます。文字列の添字結果はbyteです。",
		syntax:      "sequence[index] / mapping[key]", example: "xs := []int{4, 5}\nprintln(xs[1]) // 5\nm := map[string]int{}\nv, ok := m[\"x\"]\nprintln(v, ok) // 0 false",
		caution: "同じ構文のf[T]がジェネリックな型引数を表す場合もあり、構文だけでは確定しません。添字式なら配列等は0以上len未満が必要で、mapの未登録キーはゼロ値です。", reference: "https://go.dev/ref/spec#Index_expressions",
	},
	"slice": {
		category: "式 > スライス", title: "x[low:high]：部分列",
		description: "lowを含みhighを含まない範囲を取り出します。省略時のlowは0、highはlenです。スライスの結果は通常元の配列を共有します。",
		syntax:      "x[low:high] / x[:high] / x[low:] / x[:]", example: "xs := []int{1, 2, 3}\nys := xs[1:3]\nys[0] = 9\nprintln(xs[1], len(ys)) // 9 2",
		caution: "スライスではhighはcapまで指定できます。文字列ではバイト境界で切り、配列のスライスにはアドレス可能な配列が必要です。", reference: "https://go.dev/ref/spec#Slice_expressions",
	},
	"full_slice": {
		category: "式 > 容量制限スライス", title: "x[low:high:max]：容量も指定",
		description: "長さをhigh-low、容量をmax-lowとするスライスを作ります。appendによる元の配列の後方への書込みを制限できます。",
		syntax:      "x[low:high:max]", example: "xs := []int{1, 2, 3}\nys := xs[:1:1]\nys = append(ys, 9)\nprintln(xs[1], ys[1]) // 2 9",
		caution: "文字列には使えません。0 <= low <= high <= max <= cap(x)が必要で、省略できるのはlowのみです。", reference: "https://go.dev/ref/spec#Slice_expressions",
	},
	"composite_literal": {
		category: "式 > 複合リテラル", title: "T{...}：複合値の初期化",
		description: "構造体・配列・スライス・mapの値を要素やフィールドで初期化します。省略された要素は型のゼロ値になります。",
		syntax:      "T{elements} / T{key: value}", example: "p := struct{ X, Y int }{X: 3}\nprintln(p.X, p.Y) // 3 0",
		caution: "構造体のキー付きと位置指定は混ぜられません。他パッケージの非公開フィールドは指定できません。", reference: "https://go.dev/ref/spec#Composite_literals",
	},
	"type_assertion": {
		category: "式 > 型アサーション", title: "x.(T)：動的な型を確認",
		description: "interface値が指定する型として扱えるかを確認します。2値形式なら失敗してもゼロ値とfalseを受け取れます。",
		syntax:      "v := x.(T) / v, ok := x.(T)", example: "var x any = 3\nv, ok := x.(string)\nprintln(v == \"\", ok) // true false",
		caution: "1値形式は失敗するとパニックです。通常の型変換T(x)とは異なり、左辺はinterfaceの値である必要があります。", reference: "https://go.dev/ref/spec#Type_assertions",
	},
	"type_alias": {
		category: "宣言 > 型の別名", title: "type A = T：同一型の別名",
		description: "既存の型に別の名前を付けます。新しい定義型ではなく、AとTは同一の型を表します。",
		syntax:      "type Alias = ExistingType", example: "type ID = int\nvar n ID = 3\nvar m int = n\nprintln(m) // 3",
		caution: "=のないtype ID intは別の定義型です。型パラメータ付き別名はGo 1.24以降の機能です。", reference: "https://go.dev/ref/spec#Alias_declarations",
	},
	"variadic_param": {
		category: "宣言 > 可変長パラメータ", title: "...T：0個以上の引数",
		description: "最後のパラメータを可変長にします。関数内では[]Tとして受け取り、引数が0個ならnilスライスになります。",
		syntax:      "func name(prefix T, values ...U)", example: "func total(ns ...int) int {\n\tsum := 0\n\tfor _, n := range ns { sum += n }\n\treturn sum // total(1, 2) は3\n}",
		caution: "可変長パラメータは最後の1個だけです。...Tはそれ自体がスライス型の記法ではありません。", reference: "https://go.dev/ref/spec#Passing_arguments_to_..._parameters",
	},
	"argument_spread": {
		category: "式 > 引数展開", title: "xs...：スライスを可変長引数に",
		description: "可変長パラメータに対応する最後の引数としてスライスをそのまま渡します。渡すための新しいスライスは作りません。",
		syntax:      "f(slice...) / append(dst, src...)", example: "xs := []int{2, 3}\nys := append([]int{1}, xs...)\nprintln(len(ys), ys[2]) // 3 3",
		caution: "配列を直接展開できません。通常の可変長引数と展開を同じ可変長パラメータに混在させることもできません。", reference: "https://go.dev/ref/spec#Passing_arguments_to_..._parameters",
	},
	"array_length": {
		category: "型 > 配列長推論", title: "[...]T：要素から配列長を決定",
		description: "配列の複合リテラルで最大の要素添字に1を足した長さを推論します。結果は固定長配列です。",
		syntax:      "[...]T{elements}", example: "a := [...]int{2: 7}\nprintln(len(a), a[0], a[2]) // 3 0 7",
		caution: "var a [...]intのように初期化なしの一般の配列型としては使えません。スライスにもなりません。", reference: "https://go.dev/ref/spec#Composite_literals",
	},
	"raw_string": {
		category: "リテラル > raw文字列", title: "バッククォートの文字列",
		description: "raw文字列リテラルはエスケープを解釈せず、改行も値に含めます。パスや複数行のテキストに向いています。",
		syntax:      "`raw text`", example: "s := `a\nb`\nprintln(len(s)) // 3（改行も1バイト）",
		caution: "バッククォート自体は含められません。CR文字は値から除かれ、\\nは改行ではなく2文字になります。", reference: "https://go.dev/ref/spec#String_literals",
	},
	"interpreted_string": {
		category: "リテラル > 解釈付き文字列", title: "二重引用符の文字列",
		description: "解釈付き文字列リテラルは\\nや\\tなどを対応するバイト列に変換します。Unicodeのエスケープも使えます。",
		syntax:      "\"text\\n\" / \"\\u3042\"", example: "s := \"a\\nb\"\nprintln(len(s)) // 3",
		caution: "リテラル内に直接改行は書けません。\\xは1バイト、\\uや\\UはコードポイントのUTF-8表現を生成します。", reference: "https://go.dev/ref/spec#String_literals",
	},
	"true": {
		category: "値 > 真偽値", title: "true：真",
		description: "真偽値の真を表す事前宣言された型なし定数です。boolを必要とする条件式や変数初期化で使います。",
		syntax:      "true", example: "ready := true\nprintln(ready) // true",
		caution: "予約語ではないため同名宣言で隠せます。1という整数と同一ではありません。", reference: "https://go.dev/ref/spec#Boolean_types",
	},
	"false": {
		category: "値 > 真偽値", title: "false：偽",
		description: "真偽値の偽を表す事前宣言された型なし定数です。boolの変数のゼロ値もfalseです。",
		syntax:      "false", example: "var ready bool\nprintln(ready == false) // true",
		caution: "文字列\"false\"や整数0とは別です。同名宣言による隠蔽が可能です。", reference: "https://go.dev/ref/spec#Boolean_types",
	},
	"nil": {
		category: "値 > nil", title: "nil：値がない状態",
		description: "ポインタ・関数・slice・map・チャネル・interfaceで値がない状態を表す事前宣言された識別子です。型によって使える操作が違います。",
		syntax:      "var p *T = nil / value == nil", example: "var xs []int\nvar m map[string]int\nprintln(xs == nil, len(xs), m[\"x\"]) // true 0 0",
		caution: "nilだけでは変数の型を推論できません。nilのmapに書くとパニック、nilチャネルの送受信は待機、型付きnil入りinterfaceは非nilです。", reference: "https://go.dev/ref/spec#The_zero_value",
	},
	"_": {
		category: "名前 > 空白識別子", title: "_：値を捨てる",
		description: "空白識別子は不要な代入結果などを受け捨てる特別な名前です。新しい変数を作らず、値を読み出すこともできません。",
		syntax:      "_, value := expression / _ = expression", example: "for _, v := range []int{4} {\n\tprintln(v) // 4\n}",
		caution: "_だけでは:=の新しい変数の条件を満たしません。エラーを捨てる場合は失敗を無視してよいか確認します。", reference: "https://go.dev/ref/spec#Blank_identifier",
	},
	"iota": {
		category: "宣言 > 定数の連番", title: "iota：const内の番号",
		description: "const宣言の各ConstSpecに0から付く番号を表します。同じ定数指定内では同じ値で、次のconstグループでは0に戻ります。",
		syntax:      "const ( A = iota; B; C )", example: "const (\n\tfirst = iota\n\tsecond\n)\nprintln(first, second) // 0 1",
		caution: "ソースの物理的な改行数ではなく定数指定ごとに増えます。const宣言外では使えません。", reference: "https://go.dev/ref/spec#Iota",
	},
	"builtin_len": {
		category: "組込み > 長さ", title: "len：長さを調べる",
		description: "構文だけから組込み関数lenと推定した説明です。文字列のバイト数、配列・slice・mapの要素数、チャネルの待機中の要素数をintで返します。",
		syntax:      "len(value)", example: "var xs []int\nprintln(len(\"あ\"), len(xs)) // 3 0",
		caution: "同名の宣言があれば別の関数です。nilのslice・map・チャネルは0で、文字数とは限りません。", reference: "https://go.dev/ref/spec#Length_and_capacity",
	},
	"builtin_cap": {
		category: "組込み > 容量", title: "cap：容量を調べる",
		description: "構文だけから組込み関数capと推定した説明です。配列の長さ、sliceが現在位置から利用できる基底配列の容量、チャネルのバッファ容量を返します。",
		syntax:      "cap(arrayOrSliceOrChannel)", example: "xs := make([]int, 2, 5)\nprintln(len(xs), cap(xs)) // 2 5",
		caution: "同名宣言なら別の意味です。mapや文字列には使えず、nilのslice・チャネルは0です。", reference: "https://go.dev/ref/spec#Length_and_capacity",
	},
	"builtin_make": {
		category: "組込み > 参照型の初期化", title: "make：slice・map・チャネルを作る",
		description: "構文だけから組込み関数makeと推定した説明です。指定したslice・map・チャネル型の値を初期化して返します。ポインタを返すnewとは異なります。",
		syntax:      "make([]T, len, cap) / make(map[K]V, hint) / make(chan T, cap)", example: "m := make(map[string]int)\nm[\"x\"] = 3\nprintln(m[\"x\"]) // 3",
		caution: "同名宣言による隠蔽に注意します。sliceの長さは容量以下にし、mapのサイズ指定は要素数を増やしません。", reference: "https://go.dev/ref/spec#Making_slices_maps_and_channels",
	},
	"builtin_new": {
		category: "組込み > 割当て", title: "new：初期化した値へのポインタ",
		description: "構文だけから組込み関数newと推定した説明です。new(T)はTのゼロ値を持つ変数を確保して*Tを返します。Go 1.26以降はnew(式)による値での初期化もあります。",
		syntax:      "new(T) / new(expression)（式形式はGo 1.26以降）", example: "p := new(int)\nprintln(*p) // 0\nm := new(map[string]int)\nprintln(*m == nil) // true",
		caution: "同名宣言なら別の意味です。new(map[K]V)の参照先はnilのmapで、そのまま書込みはできません。makeとは用途が違います。", reference: "https://go.dev/ref/spec#Allocation",
	},
	"builtin_append": {
		category: "組込み > スライス追加", title: "append：要素を追加",
		description: "構文だけから組込み関数appendと推定した説明です。スライスへ要素を追加し、更新されたスライスを返します。容量が足りなければ新しい基底配列を使います。",
		syntax:      "s = append(s, values...) / s = append(s, a, b)", example: "var xs []int\nxs = append(xs, 2, 3)\nprintln(len(xs), xs[1]) // 2 3",
		caution: "同名宣言に注意します。必ず戻り値を使い、他のスライスとの基底配列共有も考慮します。追加後の容量の増え方は仕様で固定されていません。", reference: "https://go.dev/ref/spec#Appending_and_copying_slices",
	},
	"builtin_copy": {
		category: "組込み > スライスコピー", title: "copy：既存領域へコピー",
		description: "構文だけから組込み関数copyと推定した説明です。slice間でmin(len(dst), len(src))個をコピーし、その個数を返します。領域が重なっていても使えます。",
		syntax:      "n := copy(dst, src)", example: "dst := make([]int, 2)\nn := copy(dst, []int{4, 5, 6})\nprintln(n, dst[1]) // 2 5",
		caution: "同名宣言なら別の意味です。dstは自動で伸びず、nilや長さ0のdstにはコピーされません。[]byteには文字列からもコピーできます。", reference: "https://go.dev/ref/spec#Appending_and_copying_slices",
	},
	"builtin_delete": {
		category: "組込み > マップ削除", title: "delete：キーを削除",
		description: "構文だけから組込み関数deleteと推定した説明です。mapから指定したキーと値の対応を取り除きます。結果の値は返しません。",
		syntax:      "delete(mapping, key)", example: "m := map[string]int{\"x\": 3}\ndelete(m, \"x\")\nprintln(len(m)) // 0",
		caution: "同名宣言に注意します。存在しないキーやnilのmapに対しては何もしません。並行アクセスの同期は別途必要です。", reference: "https://go.dev/ref/spec#Deletion_of_map_elements",
	},
	"builtin_clear": {
		category: "組込み > 消去", title: "clear：map消去・sliceゼロ化",
		description: "構文だけから組込み関数clearと推定した説明です。Go 1.21以降、mapの全要素を削除し、sliceでは長さ分の要素をゼロ値にします。",
		syntax:      "clear(mapping) / clear(slice)", example: "xs := []int{1, 2}\nclear(xs)\nprintln(len(xs), xs[0]) // 2 0",
		caution: "同名宣言なら別の意味です。sliceの長さや容量は変わりません。nilに対しては何もせず、配列には直接使えません。", reference: "https://go.dev/ref/spec#Clear",
	},
	"builtin_close": {
		category: "組込み > チャネル終了", title: "close：送信完了を通知",
		description: "構文だけから組込み関数closeと推定した説明です。以降送信しないことをチャネルに記録します。受信側はバッファを読み終えるとゼロ値とfalseを受け取ります。",
		syntax:      "close(channel)", example: "ch := make(chan int)\nclose(ch)\nv, ok := <-ch\nprintln(v, ok) // 0 false",
		caution: "同名宣言に注意します。二重close、nilのclose、閉鎖後の送信はパニックです。受信専用型では使えず、通常は送信側が閉じます。", reference: "https://go.dev/ref/spec#Close",
	},
	"builtin_panic": {
		category: "組込み > 異常終了", title: "panic：通常の制御を中断",
		description: "構文だけから組込み関数panicと推定した説明です。通常の実行を中断し、現在のゴルーチンの呼出しを遡ってdeferを実行します。",
		syntax:      "panic(value)", example: "func fail() {\n\tdefer func() { println(recover() == \"stop\") }()\n\tpanic(\"stop\") // fail() はtrueを表示\n}",
		caution: "同名宣言に注意します。回復しなければプログラムが終了します。通常の予想可能な失敗にはerrorを返す設計を検討します。", reference: "https://go.dev/ref/spec#Handling_panics",
	},
	"builtin_recover": {
		category: "組込み > パニック回復", title: "recover：パニックを捕捉",
		description: "構文だけから組込み関数recoverと推定した説明です。同じゴルーチンでパニック中のdefer関数から直接呼ぶと、その値を受け取り巻戻しを止められます。",
		syntax:      "defer func() { v := recover(); ... }()", example: "func guarded() {\n\tdefer func() { println(recover() == \"x\") }()\n\tpanic(\"x\") // guarded() はtrueを表示\n}",
		caution: "同名宣言なら別の意味です。通常時や別ゴルーチンでは回復できません。defer関数からさらに呼んだ補助関数内のrecoverも回復しません。", reference: "https://go.dev/ref/spec#Handling_panics",
	},
	"builtin_min": {
		category: "組込み > 最小値", title: "min：最小を選ぶ",
		description: "構文だけから組込み関数minと推定した説明です。Go 1.21以降、1個以上の順序付きの値から最小値を返します。文字列にも使えます。",
		syntax:      "min(x, y, ...)", example: "println(min(3, 1, 2)) // 1\nprintln(min(\"b\", \"a\")) // a",
		caution: "同名宣言に注意します。可変長関数ではないのでスライスを...で展開できません。NaNを含む浮動小数点では結果もNaNです。", reference: "https://go.dev/ref/spec#Min_and_max",
	},
	"builtin_max": {
		category: "組込み > 最大値", title: "max：最大を選ぶ",
		description: "構文だけから組込み関数maxと推定した説明です。Go 1.21以降、1個以上の順序付きの値から最大値を返します。",
		syntax:      "max(x, y, ...)", example: "println(max(3, 1, 2)) // 3\nprintln(max(\"b\", \"a\")) // b",
		caution: "同名宣言に注意します。引数なしやslice...では呼べません。数値と文字列など互換性のない型は混ぜられません。", reference: "https://go.dev/ref/spec#Min_and_max",
	},
	"builtin_complex": {
		category: "組込み > 複素数の構築", title: "complex：実部と虚部を組み立てる",
		description: "構文だけから組込み関数complexと推定した説明です。実部と虚部の浮動小数点値から対応する複素数を作ります。定数引数なら定数になることもあります。",
		syntax:      "complex(realPart, imaginaryPart)", example: "z := complex(1.0, 2.0)\nprintln(z == 1+2i) // true",
		caution: "同名宣言に注意します。型付きの2つの引数は同じ浮動小数点型である必要があります。", reference: "https://go.dev/ref/spec#Complex_numbers",
	},
	"builtin_real": {
		category: "組込み > 複素数の実部", title: "real：実部を取り出す",
		description: "構文だけから組込み関数realと推定した説明です。複素数の実部を返します。complex64ならfloat32、complex128ならfloat64です。",
		syntax:      "real(complexValue)", example: "z := 3 + 4i\nprintln(real(z) == 3) // true",
		caution: "同名宣言なら別の意味です。絶対値を返す操作ではありません。", reference: "https://go.dev/ref/spec#Complex_numbers",
	},
	"builtin_imag": {
		category: "組込み > 複素数の虚部", title: "imag：虚部を取り出す",
		description: "構文だけから組込み関数imagと推定した説明です。複素数の虚部の係数を、対応する浮動小数点型の値として返します。",
		syntax:      "imag(complexValue)", example: "z := 3 + 4i\nprintln(imag(z) == 4) // true",
		caution: "同名宣言に注意します。結果にはiが付かず、虚部の係数だけです。", reference: "https://go.dev/ref/spec#Complex_numbers",
	},
	"builtin_print": {
		category: "組込み > デバッグ出力", title: "print：簡易出力",
		description: "構文だけから組込み関数printと推定した説明です。実装依存の形式で引数を出力する、主にデバッグ用の組込みです。",
		syntax:      "print(values)", example: "print(\"Go\")\n// 一般的な実装ではGoと表示（形式は実装依存）",
		caution: "同名宣言に注意します。出力形式は仕様で固定されず、製品の出力にはfmt等を使います。", reference: "https://go.dev/ref/spec#Bootstrapping",
	},
	"builtin_println": {
		category: "組込み > デバッグ出力", title: "println：改行付き簡易出力",
		description: "構文だけから組込み関数printlnと推定した説明です。printに類似し、引数の間の空白と末尾の改行を加えるデバッグ用の組込みです。",
		syntax:      "println(values)", example: "println(\"Go\", 3)\n// 一般的な実装ではGo 3と改行（詳細な形式は実装依存）",
		caution: "同名宣言なら別の意味です。形式や出力先の仕様を必要とする用途にはfmt等を使います。", reference: "https://go.dev/ref/spec#Bootstrapping",
	},
	"type_bool": {
		category: "型 > 真偽値", title: "bool：真または偽",
		description: "真偽値trueとfalseを持つ事前宣言された型です。条件式や論理演算の結果に使い、ゼロ値はfalseです。",
		syntax:      "var flag bool", example: "var flag bool\nprintln(flag, !flag) // false true",
		caution: "構文上の型名候補です。同名宣言で隠せます。整数との暗黙変換や大小比較はできません。", reference: "https://go.dev/ref/spec#Boolean_types",
	},
	"type_string": {
		category: "型 > 文字列", title: "string：変更できないバイト列",
		description: "変更できないバイト列の型で、ゼロ値は空文字列です。通常UTF-8のテキストを扱いますが、任意のバイトも格納できます。",
		syntax:      "var s string", example: "s := \"あ\"\nprintln(len(s), len([]rune(s))) // 3 1",
		caution: "構文上の型名候補です。添字はbyteを返し、lenはバイト数です。要素への代入はできません。", reference: "https://go.dev/ref/spec#String_types",
	},
	"type_int": {
		category: "型 > 符号付き整数", title: "int：実装依存幅の整数",
		description: "32または64ビットの符号付き整数型です。通常の整数変数やlenの結果などに使い、ゼロ値は0です。",
		syntax:      "var n int", example: "var n int = -3\nprintln(n + 1) // -2",
		caution: "構文上の型名候補です。int32やint64とは別の型で、環境によって幅が変わります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_int8": {
		category: "型 > 符号付き整数", title: "int8：8ビット整数",
		description: "-128から127までを表す符号付き整数型です。幅が固定されており、小さい整数値の保存に使えます。",
		syntax:      "var n int8", example: "var n int8 = 127\nn++\nprintln(n) // -128",
		caution: "構文上の型名候補です。範囲外の定数は代入できませんが、実行時の加算ではオーバーフローします。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_int16": {
		category: "型 > 符号付き整数", title: "int16：16ビット整数",
		description: "-32768から32767までを表す符号付き整数型です。ゼロ値は0です。",
		syntax:      "var n int16", example: "var n int16 = 300\nprintln(n * 2) // 600",
		caution: "構文上の型名候補です。異なる幅の整数と混ぜて計算する場合は明示的な変換が必要になることがあります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_int32": {
		category: "型 > 符号付き整数", title: "int32：32ビット整数",
		description: "-2147483648から2147483647までを表す符号付き整数型です。runeはこの型の別名です。",
		syntax:      "var n int32", example: "var n int32 = 100000\nprintln(n + 1) // 100001",
		caution: "構文上の型名候補です。intと幅が一致する環境でも同一の型とは限りません。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_int64": {
		category: "型 > 符号付き整数", title: "int64：64ビット整数",
		description: "-9223372036854775808から9223372036854775807までを表す符号付き整数型です。大きな整数を固定幅で扱えます。",
		syntax:      "var n int64", example: "var n int64 = 1 << 40\nprintln(n) // 1099511627776",
		caution: "構文上の型名候補です。任意精度ではなく、演算が幅を超えるとオーバーフローします。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uint": {
		category: "型 > 符号なし整数", title: "uint：実装依存幅の非負整数",
		description: "intと同じ32または64ビット幅の符号なし整数型です。0以上の値を表し、ゼロ値は0です。",
		syntax:      "var n uint", example: "var n uint = 3\nprintln(n << 1) // 6",
		caution: "構文上の型名候補です。負数は表せず、減算でも0未満にならず剰余で折り返します。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uint8": {
		category: "型 > 符号なし整数", title: "uint8：8ビット非負整数",
		description: "0から255までを表す符号なし整数型です。byteはこの型の別名で、バイト単位のデータに使います。",
		syntax:      "var n uint8", example: "var n uint8 = 255\nn++\nprintln(n) // 0",
		caution: "構文上の型名候補です。演算は256を法として折り返します。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uint16": {
		category: "型 > 符号なし整数", title: "uint16：16ビット非負整数",
		description: "0から65535までを表す固定幅の符号なし整数型です。ゼロ値は0です。",
		syntax:      "var n uint16", example: "var n uint16 = 65535\nprintln(n >> 8) // 255",
		caution: "構文上の型名候補です。整数型の幅を狭める変換では上位ビットが失われます。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uint32": {
		category: "型 > 符号なし整数", title: "uint32：32ビット非負整数",
		description: "0から4294967295までを表す固定幅の符号なし整数型です。ビット列の計算などに使えます。",
		syntax:      "var n uint32", example: "var n uint32 = 1 << 31\nprintln(n) // 2147483648",
		caution: "構文上の型名候補です。int32と同じビット幅でも値の範囲と型は異なります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uint64": {
		category: "型 > 符号なし整数", title: "uint64：64ビット非負整数",
		description: "0から18446744073709551615までを表す固定幅の符号なし整数型です。ゼロ値は0です。",
		syntax:      "var n uint64", example: "var n uint64 = 1 << 63\nprintln(n) // 9223372036854775808",
		caution: "構文上の型名候補です。int64に変換しても必ず非負のままとは限りません。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_uintptr": {
		category: "型 > アドレス用整数", title: "uintptr：ポインタのビットを保持できる整数",
		description: "ポインタのビットパターンを格納できる大きさの符号なし整数型です。低水準操作に使われますが、ポインタそのものではありません。",
		syntax:      "var n uintptr", example: "var n uintptr\nprintln(n == 0) // true",
		caution: "構文上の型名候補です。uintptrは参照先を生存させません。unsafeでの変換には別途厳しい制約があります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_byte": {
		category: "型 > バイト", title: "byte：uint8の別名",
		description: "uint8と同じ型を表す別名です。文字列の添字や[]byteで、文字ではなく1バイトを扱う意図を表します。",
		syntax:      "var b byte / []byte(s)", example: "s := \"Go\"\nvar b byte = s[0]\nprintln(b) // 71",
		caution: "構文上の型名候補です。日本語などはUTF-8で複数バイトになるので、byte1個が文字1個とは限りません。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_rune": {
		category: "型 > コードポイント", title: "rune：int32の別名",
		description: "int32と同じ型を表す別名です。Unicodeコードポイントを扱う意図を示し、文字列のrangeの第2値にも使われます。",
		syntax:      "var r rune / []rune(s)", example: "var r rune = 'あ'\nprintln(r) // 12354",
		caution: "構文上の型名候補です。runeは任意のint32を格納でき、必ず有効なUnicodeとは限りません。見た目の1文字が複数runeの場合もあります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_float32": {
		category: "型 > 浮動小数点", title: "float32：32ビット浮動小数点",
		description: "IEEE 754の32ビット浮動小数点数の型です。有限の精度と範囲を持ち、ゼロ値は0です。",
		syntax:      "var x float32", example: "var x float32 = 1.5\nprintln(x * 2 == 3) // true",
		caution: "構文上の型名候補です。全ての小数や大きな整数を正確には表現できません。float64との演算には型を揃えます。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_float64": {
		category: "型 > 浮動小数点", title: "float64：64ビット浮動小数点",
		description: "IEEE 754の64ビット浮動小数点数の型です。型を指定せず小数定数から変数を宣言すると通常この型になります。",
		syntax:      "var x float64", example: "x := 1.5\nprintln(x / 2 == 0.75) // true",
		caution: "構文上の型名候補です。10進小数の多くは丸められ、NaNは自分自身とも等しくありません。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_complex64": {
		category: "型 > 複素数", title: "complex64：float32の実部と虚部",
		description: "実部と虚部をそれぞれfloat32で表す複素数型です。四則演算や等値比較ができます。",
		syntax:      "var z complex64", example: "var z complex64 = 1 + 2i\nprintln(real(z) == 1, imag(z) == 2) // true true",
		caution: "構文上の型名候補です。大小比較や%は使えません。各成分に浮動小数点の丸めがあります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_complex128": {
		category: "型 > 複素数", title: "complex128：float64の実部と虚部",
		description: "実部と虚部をそれぞれfloat64で表す複素数型です。型なし複素数定数から変数を宣言する際の既定の型です。",
		syntax:      "var z complex128", example: "z := 1 + 2i\nprintln(z + (2 + 3i) == 3+5i) // true",
		caution: "構文上の型名候補です。浮動小数点の精度制限があり、数学上等しい計算でも結果が一致しない場合があります。", reference: "https://go.dev/ref/spec#Numeric_types",
	},
	"type_error": {
		category: "型 > エラー", title: "error：失敗の説明を返す契約",
		description: "Error() stringメソッドを持つ事前宣言されたinterface型です。慣例としてnilは失敗がないことを表します。",
		syntax:      "interface { Error() string }", example: "var err error\nprintln(err == nil) // true",
		caution: "構文上の型名候補です。nilの具体的なポインタをerrorへ格納すると、interface自体はnilではありません。", reference: "https://go.dev/ref/spec#Errors",
	},
	"type_any": {
		category: "型 > 任意の値", title: "any：空interfaceの別名",
		description: "Go 1.18以降のinterface{}の別名です。任意の型の値を保持でき、型制約では全ての型を許可します。",
		syntax:      "var x any / func f[T any](x T)", example: "var x any = 3\nv, ok := x.(int)\nprintln(v, ok) // 3 true",
		caution: "構文上の型名候補です。任意の演算を使えるという意味ではありません。比較時には動的な型が比較可能かに注意します。", reference: "https://go.dev/ref/spec#Interface_types",
	},
	"type_comparable": {
		category: "型 > 比較可能制約", title: "comparable：等値比較の型制約",
		description: "Go 1.18以降の事前宣言された型制約です。型パラメータに==と!=を使える条件を表し、mapのキー型の制約にも使います。",
		syntax:      "func equal[T comparable](a, b T) bool", example: "func equal[T comparable](a, b T) bool {\n\treturn a == b // equal(3, 3) はtrue\n}",
		caution: "構文上の型名候補です。変数の型にはできません。Go 1.20以降anyなども制約を満たす場合がありますが、動的な値が比較不能なら比較はパニックになり得ます。", reference: "https://go.dev/ref/spec#Type_constraints",
	},
}

func (note syntaxExplanation) help() syntaxHelp {
	if note.topic == "identifier" {
		return syntaxDescriptions[token.IDENT]
	}
	if help, ok := standardHelp[strings.TrimPrefix(note.topic, "standard:")]; strings.HasPrefix(note.topic, "standard:") && ok {
		return help
	}
	if help, ok := syntaxTopics[note.topic]; note.topic != "" && ok {
		return help
	}
	if note.token >= token.ADD_ASSIGN && note.token <= token.AND_NOT_ASSIGN {
		op := strings.TrimSuffix(note.token.String(), "=")
		rhs := "3"
		if note.token == token.SHL_ASSIGN || note.token == token.SHR_ASSIGN {
			rhs = "1"
		}
		result := map[token.Token]string{
			token.ADD_ASSIGN: "9", token.SUB_ASSIGN: "3", token.MUL_ASSIGN: "18",
			token.QUO_ASSIGN: "2", token.REM_ASSIGN: "0", token.AND_ASSIGN: "2",
			token.OR_ASSIGN: "7", token.XOR_ASSIGN: "5", token.SHL_ASSIGN: "12",
			token.SHR_ASSIGN: "3", token.AND_NOT_ASSIGN: "4",
		}[note.token]
		return syntaxHelp{
			category: "文 > 複合代入", title: note.token.String() + "：演算して代入",
			description: "左辺と右辺を" + op + "で演算し、結果を左辺へ代入します。左辺の式は一度だけ評価されます。",
			syntax:      "x " + note.token.String() + " y",
			example:     "x := 6\nx " + note.token.String() + " " + rhs + "\nprintln(x) // " + result,
			caution:     "x = x " + op + " yという考え方ですが、添字や関数呼出しを含む左辺の評価回数は異なります。演算に適した型が必要です。",
			reference:   "https://go.dev/ref/spec#Assignment_statements",
		}
	}
	if note.token == token.IDENT {
		switch note.literal {
		case "true", "false", "nil", "_", "iota":
			return syntaxTopics[note.literal]
		}
		for _, prefix := range []string{"builtin_", "type_"} {
			if help, ok := syntaxTopics[prefix+note.literal]; ok {
				return help
			}
		}
	}
	return syntaxDescriptions[note.token]
}
