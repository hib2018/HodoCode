package main

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func definitionScreen(t *testing.T) *screen {
	t.Helper()
	fakeScreenSDK(t, map[string]string{"time/sdk.go": "package time\ntype Time struct { N int }\n"})
	p := screenProject(t, map[string]string{
		"types.go": "package main\ntype Item struct {\n\tName string\n\tCount int\n}\n",
		"main.go":  "package main\nimport \"time\"\nfunc main() {\r\n\t名前 := []Item{{Name: `original`}}\r\n\t_ = 名前\r\n\t_ = Item{}\r\n\tvar external []time.Time\r\n\t_ = external\r\n}\r\n",
	})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func pointDefinition(t *testing.T, s *screen, marker, name string) {
	t.Helper()
	for row := range s.code {
		raw := s.rawLine(row)
		if strings.Contains(raw, marker) {
			s.cursorRow = row
			s.cursorColumn = utf8.RuneCountInString(raw[:strings.Index(raw, name)])
			s.top, s.explanationTop = 0, 0
			return
		}
	}
	t.Fatalf("missing marker %q", marker)
}

func TestDefinitionPaneSourcesAndFallback(t *testing.T) {
	s := definitionScreen(t)
	pointDefinition(t, s, "_ = 名前", "名前")
	before := s.screenPosition
	pane := strings.Join(s.explanationLines(100, 20), "\n")
	for _, want := range []string{"main.go", "4 │", "名前 := []Item{{Name: `original`}}", "types.go", "2 │ type Item struct {", "3 │     Name string"} {
		if !strings.Contains(pane, want) {
			t.Errorf("missing %q: %s", want, pane)
		}
	}
	if strings.Contains(pane, "意味\n") || s.screenPosition != before {
		t.Fatal("definition display changed cursor or retained generic help", pane)
	}
	pointDefinition(t, s, "_ = Item{}", "Item")
	pane = strings.Join(s.explanationLines(100, 20), "\n")
	if !strings.Contains(pane, "type Item struct") || strings.Contains(pane, "main.go") {
		t.Fatal(pane)
	}
	pointDefinition(t, s, "_ = external", "external")
	pane = strings.Join(s.explanationLines(100, 20), "\n")
	for _, want := range []string{"var external []time.Time", "GOROOT/src/time/sdk.go", "type Time struct { N int }"} {
		if !strings.Contains(pane, want) {
			t.Errorf("SDK definition missing %q: %s", want, pane)
		}
	}
	if strings.Contains(pane, "索引範囲外") || strings.Contains(pane, "\n意味\n") {
		t.Fatal("resolved SDK type retained fallback help", pane)
	}
}

func TestDefinitionPaneMissingSDKFallback(t *testing.T) {
	fakeScreenSDK(t, nil)
	p := screenProject(t, map[string]string{"main.go": "package main\nimport \"time\"\nfunc main() {\n var values []time.Time\n _ = values\n}\n"})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	pointDefinition(t, s, "_ = values", "values")
	before := s.screenPosition
	pane := strings.Join(s.explanationLines(200, 20), "\n")
	for _, want := range []string{"var values []time.Time", "意味", "公式リファレンス"} {
		if !strings.Contains(pane, want) {
			t.Errorf("fallback missing %q: %s", want, pane)
		}
	}
	if s.screenPosition != before || strings.Contains(pane, "GOROOT/src/") {
		t.Fatal("missing SDK lookup changed position or showed unavailable source", pane)
	}
}

func TestDefinitionPaneWrappingColorAndScrolling(t *testing.T) {
	s := definitionScreen(t)
	pointDefinition(t, s, "_ = 名前", "名前")
	wide := strings.Join(s.explanationLines(200, 20), "")
	for _, width := range []int{1, 2, 11, 38} {
		lines, styles := s.localExplanation(width, 20)
		if strings.Join(lines, "") != wide {
			t.Fatalf("width %d lost source: %q", width, lines)
		}
		for i, line := range lines {
			if displayWidth(line) > max(2, width) {
				t.Fatalf("overflow: %q", line)
			}
			for _, span := range styles[i] {
				if span.start < 0 || span.end > displayWidth(line) {
					t.Fatalf("bad span: %+v for %q", span, line)
				}
			}
		}
	}
	colored := s.render(120, 18, true)
	if !strings.Contains(colored, "\x1b[35mtype") {
		t.Fatal("missing source keyword color", colored)
	}
	t.Setenv("NO_COLOR", "1")
	plain := s.render(120, 18, false)
	if strings.Contains(plain, "\x1b[35m") || strings.Contains(plain, "\x1b[0m") {
		t.Fatal("plain render emitted color", plain)
	}
	// Keep the cursor visible while scrolling only the right pane.
	s.top = s.cursorRow
	position := s.screenPosition
	for range 300 {
		press(t, s, "]")
		s.render(41, 8, false)
	}
	if s.explanationTop == 0 || s.cursorRow != position.cursorRow || s.current != position.current {
		t.Fatal("definition scrolling moved source")
	}
	press(t, s, "[")
}

func TestDefinitionPaneDoesNotReplaceConversation(t *testing.T) {
	s := definitionScreen(t)
	s.agent = newAgentPane(agentOptions{})
	s.agent.visible = true
	s.agent.text = "saved conversation"
	pointDefinition(t, s, "_ = 名前", "名前")
	want := s.paneLines(100, 20)
	press(t, s, "right")
	if !reflect.DeepEqual(s.paneLines(100, 20), want) {
		t.Fatal("cursor overwrote conversation")
	}
	agentTestKey(t, s, "s")
	if s.agent.visible || !strings.Contains(strings.Join(s.paneLines(100, 20), "\n"), "type Item struct") {
		t.Fatal("s did not restore local definitions")
	}
	if s.agent.text != "saved conversation" {
		t.Fatal("conversation lost")
	}
}
