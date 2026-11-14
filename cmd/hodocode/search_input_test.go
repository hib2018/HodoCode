package main

import (
	"strings"
	"testing"
)

func TestCodeSearchOverlapsAndInputControls(t *testing.T) {
	s := &screen{code: []string{"1 │ aaaa"}}
	s.findCode("aa", 3, 20)
	if len(s.search.matches) != 3 {
		t.Fatal("overlapping matches missing")
	}
	if line := s.codeLine(0, 20, true); strings.Count(line, "\x1b[1;30;46ma") != 2 {
		t.Fatalf("active overlap lost priority: %q", line)
	}
	s.search.input = &agentInput{}
	for _, b := range []byte("日本語\x1b[A") {
		s.searchInputByte(b, 3, 20)
	}
	if s.search.input == nil || s.search.input.text != "日本語" {
		t.Fatal("arrow sequence changed search")
	}
	s.searchInputByte(0x15, 3, 20)
	if s.search.input.text != "" {
		t.Fatal("Ctrl+U did not clear")
	}
	for _, b := range []byte(strings.Repeat("日本語", 100)) {
		s.searchInputByte(b, 3, 20)
	}
	if prompt := s.searchPrompt(20); displayWidth(prompt) != 20 || !strings.Contains(prompt, "本語") {
		t.Fatalf("long search prompt: %q", prompt)
	}
	for _, b := range []byte{3, 4} {
		s.search.input = &agentInput{}
		if !s.searchInputByte(b, 3, 20) || s.search.input == nil {
			t.Fatal("quit shortcut escaped search input")
		}
	}
}
