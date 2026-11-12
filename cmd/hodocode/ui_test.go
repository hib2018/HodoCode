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
				for _, want := range []string{"HodoCode", "構文の解説", "NORMAL", "q 終了"} {
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
	agentTestKey(t, s, "e")
	frame := sgrPattern.ReplaceAllString(s.render(120, 24, true), "")
	for _, want := range []string{"AGENT  エージェントの解説", "e AI送信", "s 構文"} {
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

func TestUIBarAndHelp(t *testing.T) {
	for width := 41; width < 240; width++ {
		help := screenHelp(width)
		if displayWidth(help) > width || !strings.Contains(help, "q 終了") || !strings.Contains(help, "e AI送信") {
			t.Fatalf("help at %d: %q", width, help)
		}
		bar := uiBar("日本語"+strings.Repeat("x", 300), " 42% · 解説 1/3 ", width)
		if displayWidth(bar) != width || !strings.HasSuffix(bar, " 42% · 解説 1/3 ") {
			t.Fatalf("bar at %d: %q", width, bar)
		}
	}
}
