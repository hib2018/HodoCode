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
		return "構文の解説 · LOCAL"
	}
	if s.agent.cancel != nil {
		return "AGENT  解説を生成中"
	}
	return "AGENT  エージェントの解説"
}

func screenHelp(width int) string {
	if width < 80 {
		return " hjkl 移動 · i 指示 · e AI送信 · q 終了"
	}
	if width < 140 {
		return " hjkl 移動 · Enter 進む · b 戻る · i 指示 · e AI送信 · s 構文 · [] 解説 · q 終了"
	}
	return " hjkl 移動   w/B 単語   0/$ 行頭末   g/G 先頭末尾   n/N 呼出   Enter 進む   b 戻る   i 追加指示   e AI送信   s 構文   [] 解説   q 終了"
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
	if text == "構文の解説" || strings.HasPrefix(text, "カーソル位置") {
		style = "38;5;245"
	}
	return uiPaint(displaySlice(text, 0, width), style, color)
}
