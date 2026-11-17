package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestScreenChromeLayout(t *testing.T) {
	s := testScreen(t, "Recur")
	positions := regexp.MustCompile("\x1b\\[[0-9;]*H")
	for _, size := range [][2]int{{41, 8}, {80, 12}, {120, 24}, {200, 48}} {
		for _, color := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/color=%v", size[0], size[1], color), func(t *testing.T) {
				frame := s.render(size[0], size[1], color)
				plain := sgrPattern.ReplaceAllString(frame, "")
				if !color && frame != plain {
					t.Fatal("NO_COLOR emitted theme styling")
				}
				rows := 0
				for _, row := range positions.Split(plain, -1) {
					if row == "" {
						continue
					}
					rows++
					if displayWidth(row) != size[0] {
						t.Fatalf("row width %d, want %d: %q", displayWidth(row), size[0], row)
					}
				}
				if rows != size[1] {
					t.Fatalf("rows %d, want %d", rows, size[1])
				}
				for _, want := range []string{"構文", "NORMAL", "q 終了"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("missing %q", want)
					}
				}
			})
		}
	}
}

func TestScreenContextualChrome(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.options.excludes = []string{"main.go"}
	agentTestKey(t, s, "E")
	frame := sgrPattern.ReplaceAllString(s.render(120, 24, true), "")
	for _, want := range []string{"チャット", "e 表示", "i 入力", "i:再入力", "E 解説", "s 構文"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("agent chrome missing %q", want)
		}
	}
	for _, obsolete := range []string{"送信前の確認", "y 送信", "n 取消"} {
		if strings.Contains(frame, obsolete) {
			t.Fatalf("obsolete confirmation chrome %q", obsolete)
		}
	}
	agentTestKey(t, s, "i")
	if !strings.Contains(s.render(120, 24, false), " INPUT ") {
		t.Fatal("input mode missing")
	}
}

func TestPaneTitles(t *testing.T) {
	s := agentTestScreen(t, nil)
	if got := s.paneTitle(); got != "構文" {
		t.Fatal(got)
	}
	s.agent.visible = true
	if got := s.paneTitle(); got != "チャット" {
		t.Fatal(got)
	}
	s.agent.cancel = func() {}
	if got := s.paneTitle(); got != "チャット · 生成中" {
		t.Fatal(got)
	}
	s.agent.lineReview = true
	if got := s.paneTitle(); got != "行質問 · 生成中" {
		t.Fatal(got)
	}
	s.agent.cancel = nil
	if got := s.paneTitle(); got != "行質問" {
		t.Fatal(got)
	}
}

func TestLineQuestionContextualHelp(t *testing.T) {
	s := agentTestScreen(t, nil)
	if got := s.help(120); !strings.Contains(got, "/検索") || !strings.Contains(got, "f:末尾") || !strings.Contains(got, "</>:幅") {
		t.Fatal(got)
	}
	agentTestKey(t, s, "?")
	agentTestKey(t, s, "C")
	for width := 41; width < 240; width++ {
		help := s.help(width)
		if displayWidth(help) > width || !strings.Contains(help, "C:閉じる") || !strings.Contains(help, "Enter:") || !strings.Contains(help, "q 終了") {
			t.Fatalf("question help at %d: %q", width, help)
		}
	}
	if help := s.help(200); !strings.Contains(help, "Space:送信対象") || !strings.Contains(help, "p:再指定（Cで閉じてコード行を選択）") {
		t.Fatal(help)
	}
	s.notice = "送信していません"
	pane := strings.Join(s.questionLines(200, 12), "\n")
	for _, want := range []string{s.notice, "q1", s.agent.questions[0].snapshot.file, "下書き", "これは何？"} {
		if !strings.Contains(pane, want) {
			t.Fatalf("question pane missing %q: %s", want, pane)
		}
	}
	if strings.Contains(pane, "C:閉じる") || strings.Contains(pane, "Space:") {
		t.Fatal("question help repeated inside pane", pane)
	}
	s.agent.questions = nil
	if help := s.help(120); !strings.Contains(help, "Cで閉じて a:質問") {
		t.Fatal(help)
	}
	if pane := strings.Join(s.questionLines(200, 12), "\n"); !strings.Contains(pane, "下書きなし") || !strings.Contains(pane, s.notice) {
		t.Fatal(pane)
	}
}

func TestInputAndGeneratingHelp(t *testing.T) {
	s := agentTestScreen(t, nil)
	s.agent.input = &agentInput{}
	for _, action := range []string{"送信", "保存"} {
		if action == "保存" {
			s.agent.input.line = 1
		}
		for width := 41; width < 240; width++ {
			help := s.help(width)
			if displayWidth(help) > width || !strings.Contains(help, "Enter:"+action) || !strings.Contains(help, "Esc:取消") {
				t.Fatalf("input help at %d: %q", width, help)
			}
		}
	}
	s.agent.input = nil
	s.agent.cancel = func() {}
	for _, review := range []bool{false, true} {
		s.agent.visible, s.agent.lineReview = true, review
		for width := 41; width < 240; width++ {
			if help := s.help(width); displayWidth(help) > width || !strings.Contains(help, "x:中止") {
				t.Fatalf("generating help at %d: %q", width, help)
			}
		}
	}
}

func TestUIBarAndHelp(t *testing.T) {
	for width := 41; width < 240; width++ {
		help := screenHelp(width)
		if displayWidth(help) > width || !strings.Contains(help, "q 終了") || !strings.Contains(help, "E 解説") || !strings.Contains(help, "v:nvim") || !strings.Contains(help, "r:再読") {
			t.Fatalf("help at %d: %q", width, help)
		}
		bar := uiBar("日本語"+strings.Repeat("x", 300), " 42% · 解説 1/3 ", width)
		if displayWidth(bar) != width || !strings.HasSuffix(bar, " 42% · 解説 1/3 ") {
			t.Fatalf("bar at %d: %q", width, bar)
		}
	}
}
