package main

// Keys are resolved standard-library targets, not source-level import aliases.
// Descriptions and signatures were checked against the installed Go documentation.
var standardHelp = map[string]syntaxHelp{
	"fmt.Print": {
		category: "標準ライブラリ > 表示", title: "fmt.Print：改行せず表示",
		description: "値を標準の書式で標準出力へ書き、書いたバイト数と書き込みエラーを返します。",
		syntax:      "func Print(a ...any) (n int, err error)",
		example: `package main
import "fmt"
func main() {
	n, err := fmt.Print("個数: ", 3)
	if err != nil { panic(err) }
	_ = n // 書いたバイト数
}`,
		caution:   "改行は自動で付きません。隣り合う引数がどちらも文字列でない場合だけ空白が入ります。戻り値のエラーを確認できます。",
		reference: "https://pkg.go.dev/fmt#Print",
	},
	"fmt.Println": {
		category: "標準ライブラリ > 表示", title: "fmt.Println：改行付きで表示",
		description: "引数の間に空白、末尾に改行を付けて標準出力へ書き、バイト数とエラーを返します。",
		syntax:      "func Println(a ...any) (n int, err error)",
		example: `package main
import "fmt"
func main() {
	n, err := fmt.Println("個数", 3)
	if err != nil { panic(err) }
	_ = n
}`,
		caution:   "組み込みprintlnはデバッグ用で書式が実装依存です。fmt.Printlnは標準出力へ書き、書き込みエラーを返す別の関数です。%dなどは解釈しません。",
		reference: "https://pkg.go.dev/fmt#Println",
	},
	"fmt.Printf": {
		category: "標準ライブラリ > 表示", title: "fmt.Printf：書式を指定して表示",
		description: "%sや%dなどの書式に従って標準出力へ書き、バイト数とエラーを返します。",
		syntax:      "func Printf(format string, a ...any) (n int, err error)",
		example: `package main
import "fmt"
func main() {
	n, err := fmt.Printf("%sは%d個\n", "りんご", 3)
	if err != nil { panic(err) }
	_ = n
}`,
		caution:   "改行は必要なら\nで指定します。書式と引数の型・個数を合わせ、外部入力は書式ではなく%sなどの引数に渡します。",
		reference: "https://pkg.go.dev/fmt#Printf",
	},
	"fmt.Fprint": {
		category: "標準ライブラリ > 書き込み", title: "fmt.Fprint：指定先へ表示",
		description: "io.Writerへ値を標準の書式で書き、書いたバイト数とエラーを返します。",
		syntax:      "func Fprint(w io.Writer, a ...any) (n int, err error)",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	var b strings.Builder
	if _, err := fmt.Fprint(&b, "個数: ", 3); err != nil { panic(err) }
	fmt.Println(b.String()) // 個数: 3
}`,
		caution:   "改行は付きません。隣り合う引数がどちらも文字列でない場合だけ空白が入ります。書き込み先によるエラーを確認してください。",
		reference: "https://pkg.go.dev/fmt#Fprint",
	},
	"fmt.Fprintln": {
		category: "標準ライブラリ > 書き込み", title: "fmt.Fprintln：指定先へ一行書く",
		description: "io.Writerへ引数を空白区切りで書き、末尾に改行を付けます。バイト数とエラーを返します。",
		syntax:      "func Fprintln(w io.Writer, a ...any) (n int, err error)",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	var b strings.Builder
	if _, err := fmt.Fprintln(&b, "個数", 3); err != nil { panic(err) }
	fmt.Print(b.String()) // 個数 3 と改行
}`,
		caution:   "書式指定子は解釈しません。引数の文字列にも改行があると行が増えます。書き込みエラーを確認してください。",
		reference: "https://pkg.go.dev/fmt#Fprintln",
	},
	"fmt.Fprintf": {
		category: "標準ライブラリ > 書き込み", title: "fmt.Fprintf：指定先へ書式付きで書く",
		description: "io.Writerへ書式に従って書き、書いたバイト数とエラーを返します。",
		syntax:      "func Fprintf(w io.Writer, format string, a ...any) (n int, err error)",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	var b strings.Builder
	if _, err := fmt.Fprintf(&b, "%s=%d\n", "個数", 3); err != nil { panic(err) }
	fmt.Print(b.String())
}`,
		caution:   "改行は自動では付きません。書式と引数を合わせ、外部入力を直接formatにしないでください。書き込みエラーも確認します。",
		reference: "https://pkg.go.dev/fmt#Fprintf",
	},
	"fmt.Sprint": {
		category: "標準ライブラリ > 文字列", title: "fmt.Sprint：値を文字列にする",
		description: "値を標準の書式で文字列にして返します。標準出力への書き込みは行いません。",
		syntax:      "func Sprint(a ...any) string",
		example: `package main
import "fmt"
func main() {
	s := fmt.Sprint("個数: ", 3)
	fmt.Println(s) // 個数: 3
}`,
		caution:   "改行は付きません。隣り合う引数がどちらも文字列でない場合だけ空白が入り、常に空白区切りにはなりません。",
		reference: "https://pkg.go.dev/fmt#Sprint",
	},
	"fmt.Sprintln": {
		category: "標準ライブラリ > 文字列", title: "fmt.Sprintln：改行付き文字列を作る",
		description: "値を空白で区切り、末尾に改行を付けた文字列を返します。出力は行いません。",
		syntax:      "func Sprintln(a ...any) string",
		example: `package main
import "fmt"
func main() {
	s := fmt.Sprintln("個数", 3)
	fmt.Print(s) // 個数 3 と改行
}`,
		caution:   "返る文字列に既に改行があります。さらにPrintlnへ渡すと余分な改行が付きます。書式指定子は解釈しません。",
		reference: "https://pkg.go.dev/fmt#Sprintln",
	},
	"fmt.Sprintf": {
		category: "標準ライブラリ > 文字列", title: "fmt.Sprintf：書式付き文字列を作る",
		description: "書式に従って値を文字列に整えて返します。出力せずに表示用の文などを組み立てられます。",
		syntax:      "func Sprintf(format string, a ...any) string",
		example: `package main
import "fmt"
func main() {
	s := fmt.Sprintf("%sは%d個", "りんご", 3)
	fmt.Println(s)
}`,
		caution:   "書式と引数の型・個数を合わせてください。SQLやHTMLなどの安全なエスケープを行う関数ではありません。",
		reference: "https://pkg.go.dev/fmt#Sprintf",
	},
	"fmt.Errorf": {
		category: "標準ライブラリ > エラー", title: "fmt.Errorf：説明付きエラーを作る",
		description: "書式からerrorを作ります。%wで元のエラーを包むと、errors.Isやerrors.Asで原因をたどれます。",
		syntax:      "func Errorf(format string, a ...any) (err error)",
		example: `package main
import (
	"errors"
	"fmt"
)
func main() {
	cause := errors.New("見つかりません")
	err := fmt.Errorf("設定の読み込み: %w", cause)
	fmt.Println(errors.Is(err, cause)) // true
}`,
		caution:   "%wの引数はerrorである必要があります。%vは文字列化だけで原因を包みません。複数の%wは複数の原因を持つエラーになります。",
		reference: "https://pkg.go.dev/fmt#Errorf",
	},
	"errors.New": {
		category: "標準ライブラリ > エラー", title: "errors.New：固定文のエラーを作る",
		description: "指定した文字列をメッセージに持つ新しいerrorを返します。書式指定子は解釈しません。",
		syntax:      "func New(text string) error",
		example: `package main
import (
	"errors"
	"fmt"
)
func main() {
	err := errors.New("入力が空です")
	fmt.Println(err.Error())
}`,
		caution:   "同じメッセージでも呼び出すたびに別のエラーです。errors.Isで判定する目印は、毎回作り直さず同じ値を共有します。",
		reference: "https://pkg.go.dev/errors#New",
	},
	"errors.Is": {
		category: "標準ライブラリ > エラー", title: "errors.Is：原因のエラーを調べる",
		description: "エラー自身とUnwrapでたどれるエラーの木を調べ、指定したエラーに一致するか返します。独自のIsメソッドによる一致も扱います。",
		syntax:      "func Is(err error, target error) bool",
		example: `package main
import (
	"errors"
	"fmt"
)
func main() {
	missing := errors.New("対象なし")
	err := fmt.Errorf("検索失敗: %w", missing)
	fmt.Println(errors.Is(err, missing)) // true
}`,
		caution:   "メッセージ文字列を比較する関数ではありません。比較可能なtargetを使います。%vで文字列化しただけの原因はたどれません。",
		reference: "https://pkg.go.dev/errors#Is",
	},
	"errors.As": {
		category: "標準ライブラリ > エラー", title: "errors.As：原因を型で取り出す",
		description: "エラーの木で最初に対象の型に合うエラーを探し、targetの指す変数へ代入してtrueを返します。独自のAsメソッドも扱います。",
		syntax:      "func As(err error, target any) bool",
		example: `package main
import (
	"errors"
	"fmt"
	"strconv"
)
func main() {
	_, cause := strconv.Atoi("abc")
	err := fmt.Errorf("個数の変換: %w", cause)
	var detail *strconv.NumError
	if errors.As(err, &detail) {
		fmt.Println(detail.Num) // abc
	}
}`,
		caution:   "targetは非nilポインタで、その指す型はerrorを実装する型かインターフェース型でなければパニックになります。例は*NumError用の変数のアドレス（**NumError）を渡します。原因を残すには%wなどで包みます。",
		reference: "https://pkg.go.dev/errors#As",
	},
	"strings.Contains": {
		category: "標準ライブラリ > 文字列", title: "strings.Contains：部分文字列を探す",
		description: "文字列sの中にsubstrが含まれていればtrueを返します。",
		syntax:      "func Contains(s string, substr string) bool",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	found := strings.Contains("日本語の本", "日本語")
	fmt.Println(found) // true
}`,
		caution:   "大文字と小文字を区別し、正規表現ではありません。空文字列はどの文字列にも含まれると判定します。",
		reference: "https://pkg.go.dev/strings#Contains",
	},
	"strings.HasPrefix": {
		category: "標準ライブラリ > 文字列", title: "strings.HasPrefix：先頭を調べる",
		description: "文字列sがprefixで始まっていればtrueを返します。",
		syntax:      "func HasPrefix(s string, prefix string) bool",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	ok := strings.HasPrefix("Go入門", "Go")
	fmt.Println(ok) // true
}`,
		caution:   "大文字と小文字を区別します。空のprefixは常に一致します。パスの安全性を文字列の先頭一致だけで判断しないでください。",
		reference: "https://pkg.go.dev/strings#HasPrefix",
	},
	"strings.HasSuffix": {
		category: "標準ライブラリ > 文字列", title: "strings.HasSuffix：末尾を調べる",
		description: "文字列sがsuffixで終わっていればtrueを返します。",
		syntax:      "func HasSuffix(s string, suffix string) bool",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	ok := strings.HasSuffix("memo.txt", ".txt")
	fmt.Println(ok) // true
}`,
		caution:   "大文字と小文字を区別します。空のsuffixは常に一致し、拡張子が合ってもファイルの内容や安全性は保証されません。",
		reference: "https://pkg.go.dev/strings#HasSuffix",
	},
	"strings.TrimSpace": {
		category: "標準ライブラリ > 文字列", title: "strings.TrimSpace：前後の空白を除く",
		description: "Unicodeで空白とされる文字を先頭と末尾から除いた文字列を返します。",
		syntax:      "func TrimSpace(s string) string",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	s := strings.TrimSpace("\t Go 入門　\n")
	fmt.Println(s) // Go 入門
}`,
		caution:   "途中の空白は消しません。元の文字列を変更するのではなく結果を返すので、必要なら代入してください。",
		reference: "https://pkg.go.dev/strings#TrimSpace",
	},
	"strings.Split": {
		category: "標準ライブラリ > 文字列", title: "strings.Split：区切って分割する",
		description: "区切り文字列sepでsを分け、区切りを含まない部分文字列のスライスを返します。",
		syntax:      "func Split(s string, sep string) []string",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	parts := strings.Split("赤,青,緑", ",")
	fmt.Println(parts[1]) // 青
}`,
		caution:   "連続する区切りは空要素を作ります。sepが空ならUTF-8の各シーケンスの後で分割します。引用符を扱うCSVの解析には使えません。",
		reference: "https://pkg.go.dev/strings#Split",
	},
	"strings.Join": {
		category: "標準ライブラリ > 文字列", title: "strings.Join：区切りを挟んで連結する",
		description: "文字列スライスの要素の間にsepを挟み、一つの文字列を返します。",
		syntax:      "func Join(elems []string, sep string) string",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	s := strings.Join([]string{"赤", "青", "緑"}, "・")
	fmt.Println(s) // 赤・青・緑
}`,
		caution:   "先頭と末尾には区切りを足しません。空スライスなら空文字列です。要素内の区切り文字をエスケープする機能はありません。",
		reference: "https://pkg.go.dev/strings#Join",
	},
	"strings.ReplaceAll": {
		category: "標準ライブラリ > 文字列", title: "strings.ReplaceAll：すべて置換する",
		description: "重なり合わないoldをすべてnewに置き換えた文字列を返します。元の文字列は変わりません。",
		syntax:      "func ReplaceAll(s string, old string, new string) string",
		example: `package main
import (
	"fmt"
	"strings"
)
func main() {
	s := strings.ReplaceAll("赤と赤", "赤", "青")
	fmt.Println(s) // 青と青
}`,
		caution:   "正規表現ではありません。oldが空だと先頭と各UTF-8シーケンスの後にnewを挿入します。結果を再帰的に置換し続けるわけではありません。",
		reference: "https://pkg.go.dev/strings#ReplaceAll",
	},
	"strconv.Atoi": {
		category: "標準ライブラリ > 数値変換", title: "strconv.Atoi：文字列をintにする",
		description: "10進の整数文字列をintへ変換します。ParseInt(s, 10, 0)の結果をintにしたものに相当します。",
		syntax:      "func Atoi(s string) (int, error)",
		example: `package main
import (
	"fmt"
	"strconv"
)
func main() {
	n, err := strconv.Atoi("42")
	if err != nil { panic(err) }
	fmt.Println(n + 1) // 43
}`,
		caution:   "空白や小数点は許されません。変換失敗やintの範囲超過のエラーを確認してください。intの範囲は環境に依存します。",
		reference: "https://pkg.go.dev/strconv#Atoi",
	},
	"strconv.Itoa": {
		category: "標準ライブラリ > 数値変換", title: "strconv.Itoa：intを文字列にする",
		description: "intの値を10進表記の文字列に変換して返します。",
		syntax:      "func Itoa(i int) string",
		example: `package main
import (
	"fmt"
	"strconv"
)
func main() {
	s := strconv.Itoa(42)
	fmt.Println("個数=" + s) // 個数=42
}`,
		caution:   "string(整数)は数の10進表記への変換ではなくUnicodeコードポイントの文字列化です。int64にはFormatIntなどを使います。",
		reference: "https://pkg.go.dev/strconv#Itoa",
	},
	"strconv.ParseInt": {
		category: "標準ライブラリ > 数値変換", title: "strconv.ParseInt：基数と範囲を指定する",
		description: "符号付き整数文字列を指定の基数とビット幅で解釈し、int64の値とエラーを返します。",
		syntax:      "func ParseInt(s string, base int, bitSize int) (i int64, err error)",
		example: `package main
import (
	"fmt"
	"strconv"
)
func main() {
	n, err := strconv.ParseInt("ff", 16, 16)
	if err != nil { panic(err) }
	fmt.Println(n) // 255
}`,
		caution:   "baseは0または2〜36です。0は接頭辞から判定し、先頭0は8進になります。bitSizeが0ならintの範囲ですが戻り値はint64です。範囲超過時も値は返るため必ずerrを確認します。",
		reference: "https://pkg.go.dev/strconv#ParseInt",
	},
	"os.Open": {
		category: "標準ライブラリ > ファイル", title: "os.Open：読み取り用に開く",
		description: "指定したファイルを読み取り専用で開き、*os.Fileとエラーを返します。失敗時のエラーは*os.PathErrorです。",
		syntax:      "func Open(name string) (*os.File, error)",
		example: `package main
import (
	"fmt"
	"os"
)
func main() {
	f, err := os.Open("input.txt")
	if err != nil { fmt.Println(err); return }
	defer func() {
		if err := f.Close(); err != nil { fmt.Println(err) }
	}()
	info, err := f.Stat()
	if err != nil { fmt.Println(err); return }
	fmt.Println(info.Size())
}`,
		caution:   "input.txtが存在する前提の例です。成功後はCloseで資源を解放します。書き込み用ではなく、存在しないファイルも作りません。",
		reference: "https://pkg.go.dev/os#Open",
	},
	"os.ReadFile": {
		category: "標準ライブラリ > ファイル", title: "os.ReadFile：ファイル全体を読む",
		description: "指定したファイルを読み、内容のバイトスライスとエラーを返します。内部で開閉を行うので呼び出し側のCloseは不要です。",
		syntax:      "func ReadFile(name string) ([]byte, error)",
		example: `package main
import (
	"fmt"
	"os"
)
func main() {
	data, err := os.ReadFile("input.txt")
	if err != nil { fmt.Println(err); return }
	fmt.Println(string(data))
}`,
		caution:   "input.txtが存在する前提です。全体をメモリへ読むので巨大なファイルに注意します。正常な終端はio.EOFではなくnilのエラーになります。",
		reference: "https://pkg.go.dev/os#ReadFile",
	},
	"os.WriteFile": {
		category: "標準ライブラリ > ファイル", title: "os.WriteFile：内容をまとめて書く",
		description: "指定ファイルへバイト列を書きます。なければ作成し、既存なら切り詰めて上書きします。結果はerrorで返ります。",
		syntax:      "func WriteFile(name string, data []byte, perm os.FileMode) error",
		example: `package main
import (
	"fmt"
	"os"
)
func main() {
	// 実行するとこのパスを上書きするため、保存先を確認する。
	err := os.WriteFile("example-output.txt", []byte("こんにちは\n"), 0o600)
	if err != nil { fmt.Println(err); return }
	fmt.Println("保存しました")
}`,
		caution:   "既存内容を失います。permは新規作成時だけ適用され、umaskの影響も受けます。既存ファイルの権限は変えません。書き込みは非アトミックで、失敗時に途中の内容が残ることがあります。",
		reference: "https://pkg.go.dev/os#WriteFile",
	},
	"os.Stat": {
		category: "標準ライブラリ > ファイル", title: "os.Stat：ファイル情報を調べる",
		description: "指定パスのサイズや更新日時などを表すos.FileInfoとエラーを返します。",
		syntax:      "func Stat(name string) (os.FileInfo, error)",
		example: `package main
import (
	"fmt"
	"os"
)
func main() {
	info, err := os.Stat("input.txt")
	if err != nil { fmt.Println(err); return }
	fmt.Println(info.Size(), info.IsDir())
}`,
		caution:   "エラーならinfoを使わないでください。存在確認後にファイルが変わる場合があるので、後続のOpenなどのエラー確認も必要です。",
		reference: "https://pkg.go.dev/os#Stat",
	},
	"io.WriteString": {
		category: "標準ライブラリ > 入出力", title: "io.WriteString：文字列を書き込む",
		description: "io.Writerへ文字列を書き、バイト数とエラーを返します。StringWriterならそのWriteStringメソッドを利用します。",
		syntax:      "func WriteString(w io.Writer, s string) (n int, err error)",
		example: `package main
import (
	"fmt"
	"io"
	"strings"
)
func main() {
	var b strings.Builder
	n, err := io.WriteString(&b, "こんにちは")
	if err != nil { panic(err) }
	fmt.Println(n, b.String())
}`,
		caution:   "バイト数は文字数とは限りません。改行は追加しません。書き込み先によって失敗するためエラーを確認してください。",
		reference: "https://pkg.go.dev/io#WriteString",
	},
	"io.ReadAll": {
		category: "標準ライブラリ > 入出力", title: "io.ReadAll：終端まで読み取る",
		description: "io.Readerからエラーまたは終端まで読み、読めたバイト列とエラーを返します。正常な終端はnilのエラーになります。",
		syntax:      "func ReadAll(r io.Reader) ([]byte, error)",
		example: `package main
import (
	"fmt"
	"io"
	"strings"
)
func main() {
	data, err := io.ReadAll(strings.NewReader("こんにちは"))
	if err != nil { panic(err) }
	fmt.Println(string(data))
}`,
		caution:   "すべてをメモリへ読むので、信頼できない入力はLimitReaderなどで制限します。Readerは閉じません。エラー時にも一部のデータが返る場合があります。",
		reference: "https://pkg.go.dev/io#ReadAll",
	},
	"io.Copy": {
		category: "標準ライブラリ > 入出力", title: "io.Copy：読み取り元から書き込み先へ転送",
		description: "srcの終端またはエラーまでdstへ転送し、転送したバイト数と最初のエラーを返します。",
		syntax:      "func Copy(dst io.Writer, src io.Reader) (written int64, err error)",
		example: `package main
import (
	"fmt"
	"io"
	"strings"
)
func main() {
	var dst strings.Builder
	n, err := io.Copy(&dst, strings.NewReader("Go入門"))
	if err != nil { panic(err) }
	fmt.Println(n, dst.String())
}`,
		caution:   "引数は書き込み先、読み取り元の順です。正常な終端はio.EOFではなくnilです。どちらも閉じず、終端のない入力では待ち続ける場合があります。",
		reference: "https://pkg.go.dev/io#Copy",
	},
	"context.Background": {
		category: "標準ライブラリ > 処理の制御", title: "context.Background：起点を作る",
		description: "キャンセルされず、期限も値もない非nilのcontext.Contextを返します。mainや初期化、テストなどの起点に使います。",
		syntax:      "func Background() context.Context",
		example: `package main
import (
	"context"
	"fmt"
)
func main() {
	ctx := context.Background()
	fmt.Println(ctx.Err()) // <nil>
}`,
		caution:   "Doneはnilで、その受信を直接待つと待ち続けます。処理の途中で既存の親contextを置き換えると、親からのキャンセルや期限を引き継げません。",
		reference: "https://pkg.go.dev/context#Background",
	},
	"context.WithCancel": {
		category: "標準ライブラリ > 処理の制御", title: "context.WithCancel：中止を伝える",
		description: "親から派生したcontextとCancelFuncを返します。cancelを呼ぶか親が終了するとDoneが閉じ、中止を通知します。",
		syntax:      "func WithCancel(parent context.Context) (ctx context.Context, cancel context.CancelFunc)",
		example: `package main
import (
	"context"
	"fmt"
)
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // すべての終了経路で解放する
	cancel()
	<-ctx.Done()
	fmt.Println(ctx.Err()) // context canceled
}`,
		caution:   "呼び出し側にはcancelを呼んで資源を解放する責任があります。複数回呼べます。通知だけでgoroutineを強制停止したり、終了を待ったりはしません。parentにnilは渡せません。",
		reference: "https://pkg.go.dev/context#WithCancel",
	},
	"context.WithTimeout": {
		category: "標準ライブラリ > 処理の制御", title: "context.WithTimeout：制限時間を付ける",
		description: "指定時間後を期限とする派生contextとCancelFuncを返します。親の期限やキャンセルが先なら、そちらで終了します。",
		syntax:      "func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc)",
		example: `package main
import (
	"context"
	"fmt"
	"time"
)
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel() // 期限前に終わっても資源を解放する
	deadline, ok := ctx.Deadline()
	fmt.Println(ok, deadline)
}`,
		caution:   "期限があってもcancelを呼ぶ責任は残ります。時間にはtime.Secondなどの単位を付けます。対象の処理がcontextを監視しなければ強制停止はできません。parentにnilは渡せません。",
		reference: "https://pkg.go.dev/context#WithTimeout",
	},
	"time.Now": {
		category: "標準ライブラリ > 時刻", title: "time.Now：現在時刻を得る",
		description: "現在のローカル時刻をtime.Timeとして返します。表示や処理時間の測定の起点に使えます。",
		syntax:      "func Now() time.Time",
		example: `package main
import (
	"fmt"
	"time"
)
func main() {
	now := time.Now()
	fmt.Println(now.Format(time.RFC3339))
}`,
		caution:   "結果は実行時刻と環境のタイムゾーンに依存します。UTCが必要ならUTC()で変換します。書式にはGoの基準時刻の配置や定数を使います。",
		reference: "https://pkg.go.dev/time#Now",
	},
	"time.Sleep": {
		category: "標準ライブラリ > 時刻", title: "time.Sleep：一定時間待つ",
		description: "現在のgoroutineを少なくとも指定時間だけ停止します。0以下の時間ならすぐに戻ります。",
		syntax:      "func Sleep(d time.Duration)",
		example: `package main
import (
	"fmt"
	"time"
)
func main() {
	time.Sleep(10 * time.Millisecond)
	fmt.Println("待機終了")
}`,
		caution:   "再開は指定時間ぴったりとは限りません。単位なしの数値はナノ秒です。他のgoroutineとの同期やキャンセル可能な待機の代用にはしないでください。",
		reference: "https://pkg.go.dev/time#Sleep",
	},
	"log.Print": {
		category: "標準ライブラリ > ログ", title: "log.Print：標準ロガーへ記録",
		description: "fmt.Printと同様に引数を整形して標準ロガーへ記録します。初期設定では日時付きで標準エラー出力へ書きます。",
		syntax:      "func Print(v ...any)",
		example: `package main
import "log"
func main() {
	count := 3
	log.Print("処理件数: ", count)
}`,
		caution:   "末尾に改行がなければロガーが追加します。書式指定子は解釈せず、書き込みエラーは返しません。出力先やフラグは共有設定で変わります。秘密情報を記録しないでください。",
		reference: "https://pkg.go.dev/log#Print",
	},
	"log.Println": {
		category: "標準ライブラリ > ログ", title: "log.Println：空白区切りで記録",
		description: "fmt.Printlnと同様に引数を空白区切り・改行付きで標準ロガーへ記録します。初期設定では日時付きの標準エラー出力です。",
		syntax:      "func Println(v ...any)",
		example: `package main
import "log"
func main() {
	count := 3
	log.Println("処理件数", count)
}`,
		caution:   "書式指定子は解釈しません。書き込みエラーを返さず、処理を終了させる関数でもありません。標準ロガーの設定変更は他の呼び出しにも影響します。",
		reference: "https://pkg.go.dev/log#Println",
	},
	"log.Printf": {
		category: "標準ライブラリ > ログ", title: "log.Printf：書式付きで記録",
		description: "fmt.Printfと同様に書式に従って標準ロガーへ記録します。初期設定では日時付きで標準エラー出力へ書きます。",
		syntax:      "func Printf(format string, v ...any)",
		example: `package main
import "log"
func main() {
	count := 3
	log.Printf("処理件数=%d", count)
}`,
		caution:   "末尾に改行がなければロガーが追加します。書式と引数を合わせ、外部入力はformatではなく引数にします。書き込みエラーは返りません。秘密情報にも注意します。",
		reference: "https://pkg.go.dev/log#Printf",
	},
}
