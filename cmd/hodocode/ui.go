package main

import "strings"

func uiPaint(text, style string, color bool) string {
	if !color {
		return text
	}
	return "\x1b[" + style + "m" + text + "\x1b[0m"
}

func (s *screen) paneTitle() string {
	if s.agent == nil || !s.agent.visible {
		return "構文"
	}
	if s.agent.lineReview {
		if s.agent.cancel != nil {
			return "行質問 · 生成中"
		}
		return "行質問"
	}
	if s.agent.cancel != nil {
		return "チャット · 生成中"
	}
	return "チャット"
}

func (s *screen) help(width int) string {
	if s.agent != nil && s.agent.input != nil {
		action := "送信"
		if s.agent.input.line != 0 || s.agent.input.draft != nil {
			action = "保存"
		}
		for _, keys := range []string{
			" Alt+Enter/Ctrl+J:改行 Esc:取消 ←/→:移動 Ctrl+U/K:削除",
			" Alt+Enter/Ctrl+J:改行 Esc:取消",
			" ^J:改行 Esc:取消",
		} {
			help := " Enter:" + action + keys
			if displayWidth(help) <= width {
				return help
			}
		}
		return displaySlice(" Enter:"+action+" Esc:取消", 0, width)
	}
	if s.agent == nil || !s.agent.visible || !s.agent.lineReview {
		help := screenHelp(width)
		extra := " f:末尾 </>:幅"
		if s.agent != nil && s.agent.cancel != nil {
			extra = " x:中止" + extra
			if displayWidth(help+extra) > width {
				help = " /検索 x:中止 q 終了"
				extra = " f:末尾 </>:幅"
			}
		}
		if displayWidth(help+extra) <= width {
			help = strings.Replace(help, " q 終了", extra+" q 終了", 1)
		}
		return help
	}
	if len(s.agent.questions) == 0 {
		if s.agent.cancel != nil {
			return displaySlice(" C:閉じる x:中止 q 終了", 0, width)
		}
		help := " Cで閉じて a:質問 / ?:これは何？ q 終了"
		if displayWidth(help) <= width {
			return help
		}
		return displaySlice(" C:閉じる q 終了", 0, width)
	}
	extra := " </>:幅"
	if s.agent.cancel != nil {
		extra = " x:中止" + extra
	}
	for _, help := range []string{
		" C:閉じる j/k:選択 Space:送信対象 i:編集 d:削除 p:再指定（Cで閉じてコード行を選択） Enter:選択分を送信",
		" C:閉じる j/k:選択 Space:対象 i:編集 d:削除 p:再指定 Enter:送信",
		" C:閉じる j/k:選択 i:編集 Enter:送信",
		" C:閉じる Enter:送信",
	} {
		if displayWidth(help+extra+" q 終了") <= width {
			return help + extra + " q 終了"
		}
	}
	if s.agent.cancel != nil {
		return displaySlice(" C:閉じる x:中止 q 終了", 0, width)
	}
	return displaySlice(" C:閉じる q 終了", 0, width)
}

func screenHelp(width int) string {
	if width < 80 {
		return " /検索 ^N/P v:nvim r:再読 E 解説 q 終了"
	}
	if width < 180 {
		return " /検索 ^N/P v:nvim r:再読 e 表示 i 入力 E 解説 s 構文 a/C質問 q 終了"
	}
	return " a/C 質問 w/B 単語 g/G 先頭末尾 n/N 呼出 /検索 ^N/P次前 Enter 進む b 戻る t 階層 v:nvim r:再読 c 行解説 e 表示 i 入力 E 解説 s 構文 [] 解説 q 終了"
}

func uiBar(left, right string, width int) string {
	if displayWidth(right)+12 >= width {
		return displaySlice(left, 0, width)
	}
	return displaySlice(left, 0, width-displayWidth(right)) + right
}

func paneTextLine(text string, width int, color bool) string {
	style := "38;5;252"
	switch text {
	case "意味", "書き方", "使用例":
		style = "1;38;5;110"
	case "注意点":
		style = "1;38;5;179"
	case "公式リファレンス":
		style = "38;5;245"
	}
	if strings.HasPrefix(text, "https://go.dev/") {
		style = "38;5;245"
	}
	return uiPaint(displaySlice(text, 0, width), style, color)
}
