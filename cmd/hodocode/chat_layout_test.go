package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hib2018/HodoCode/internal/explain"
)

func TestChatTailAndReadingPosition(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	s.agent.history = []agentMessage{{id: s.current, Message: explain.Message{Role: "assistant", Content: strings.Repeat("answer\n", 40)}}}
	lines := s.paneLines(40, 10)
	if s.explanationTop != len(lines)-10 {
		t.Fatal("chat did not follow tail")
	}
	press(t, s, "[")
	top := s.explanationTop
	s.agent.results <- agentResult{id: s.current, text: strings.Repeat("new\n", 20)}
	s.receiveAgentResult()
	s.paneLines(40, 10)
	if s.explanationTop != top || !s.agent.unread {
		t.Fatal("incoming answer moved reading position")
	}
	prompt, _, _ := s.chatPrompt(40, 10)
	if !strings.Contains(strings.Join(prompt, "\n"), "新着") {
		t.Fatal("missing new answer hint")
	}
	agentTestKey(t, s, "f")
	lines = s.paneLines(40, 10)
	if s.explanationTop != len(lines)-10 || s.agent.unread {
		t.Fatal("return to tail failed")
	}
}

func TestChatGenerationCancelAndRetry(t *testing.T) {
	started := make(chan struct{}, 1)
	s := agentTestScreen(t, func(ctx context.Context, _ string) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	})
	agentTestKey(t, s, "E")
	<-started
	agentTestKey(t, s, "x")
	agentTestResult(t, s)
	if s.agent.cancel != nil || s.agent.stopping || !strings.Contains(s.agent.text, "中断") || len(s.agent.history) != 1 {
		t.Fatal("cancel did not retain conversation")
	}
	agentTestKey(t, s, "i")
	if s.agent.input == nil {
		t.Fatal("cannot ask after cancellation")
	}
}

func TestChatResizablePaneAndPromptBounds(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	for _, width := range []int{41, 80, 120, 200} {
		s.notePercent = 0
		original := s.codeWidth(width)
		for i := 0; i < 20; i++ {
			agentTestKey(t, s, ">")
		}
		if s.codeWidth(width) >= original || s.codeWidth(width) < 20 {
			t.Fatal("wide pane bounds")
		}
		for i := 0; i < 20; i++ {
			agentTestKey(t, s, "<")
		}
		if s.codeWidth(width) <= original {
			t.Fatal("narrow pane bounds")
		}
		for _, percent := range []int{25, 50, 70} {
			s.notePercent = percent
			for _, height := range []int{8, 12, 24} {
				s.agent.input = &agentInput{id: s.current, text: strings.Repeat("日本語\n", 20)}
				prompt, row, col := s.chatPrompt(width-s.codeWidth(width)-5, height-3)
				if len(prompt) >= height-3 || row < 0 || row >= len(prompt) || col < 0 || col >= width-s.codeWidth(width)-5 {
					t.Fatalf("prompt out of bounds %dx%d: %q %d,%d", width, height, prompt, row, col)
				}
				frame := s.render(width, height, false)
				if strings.Contains(frame, "HodoCode") || strings.Contains(frame, "╭") {
					t.Fatal("redundant title or chat modal")
				}
			}
		}
	}
}

func TestChatInlineCursorAndFrameBounds(t *testing.T) {
	s := agentTestScreen(t, nil)
	agentTestKey(t, s, "e")
	positions := regexp.MustCompile(`\[(\d+);(\d+)H`)
	cursor := regexp.MustCompile(`\[(\d+);(\d+)H\[\?25h`)
	for _, size := range [][2]int{{41, 8}, {80, 12}, {120, 24}} {
		for _, percent := range []int{25, 50, 70} {
			for _, text := range []string{"", "日本語", strings.Repeat("長い行\n", 20)} {
				s.notePercent = percent
				s.agent.input = &agentInput{id: s.current, text: text}
				var out bytes.Buffer
				err := screenLoop(context.Background(), s, &out, func() (byte, bool, error) { return 0, false, io.EOF }, func() (int, int, error) { return size[0], size[1], nil }, false)
				if err != io.EOF {
					t.Fatal(err)
				}
				matches := cursor.FindAllStringSubmatch(out.String(), -1)
				if len(matches) == 0 {
					t.Fatal("missing input cursor")
				}
				last := matches[len(matches)-1]
				row, _ := strconv.Atoi(last[1])
				col, _ := strconv.Atoi(last[2])
				prompt, r, c := s.chatPrompt(size[0]-s.codeWidth(size[0])-5, size[1]-3)
				if row != size[1]-1-len(prompt)+r || col != s.codeWidth(size[0])+5+c || row < 2 || row > size[1]-2 || col > size[0] {
					t.Fatalf("cursor outside prompt %dx%d: %d,%d", size[0], size[1], row, col)
				}
				frame := s.render(size[0], size[1], false)
				for _, line := range positions.Split(strings.TrimPrefix(frame, "\x1b[H"), -1) {
					if line != "" && displayWidth(line) != size[0] {
						t.Fatal(fmt.Sprintf("frame row overflow %dx%d: %q", size[0], size[1], line))
					}
				}
			}
		}
	}
}
