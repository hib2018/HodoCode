package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
)

func treeScreen(t *testing.T) *screen {
	t.Helper()
	p := screenProject(t, map[string]string{"main.go": `package main
import "fmt"
func main() { A(); B(); A(); fmt.Println("hello"); missing() }
func A() { C() }
func B() { D() }
func C() { A() }
func D() {}
`})
	s, err := newScreen(p, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCallTreeManualExpansion(t *testing.T) {
	s := treeScreen(t)
	press(t, s, "t")
	tree := s.tree
	if !tree.visible || len(tree.rows()) != 5 || len(s.flow.expanded) != 1 {
		t.Fatal("initial view must deduplicate calls and only parse root")
	}
	press(t, s, "j")
	a := tree.selected
	if a.open || len(s.flow.expanded) != 1 {
		t.Fatal("selection must not expand")
	}
	press(t, s, "right")
	if !a.open || len(tree.rows()) != 6 || len(s.flow.expanded) != 2 {
		t.Fatal("explicit expansion failed")
	}
	press(t, s, "j", "j") // C, then B
	b := tree.selected
	if b.site.Name != "B" || !a.open || b.open {
		t.Fatal("moving to sibling must preserve open branches")
	}
	press(t, s, "l")
	if !a.open || !b.open {
		t.Fatal("multiple branches must remain open")
	}
	press(t, s, "h")
	if b.open || !a.open {
		t.Fatal("collapse must affect only selected branch")
	}
	press(t, s, "k", "l") // C; expand C to expose A as a cycle
	cycle := tree.selected.children[0]
	if !cycle.cycle {
		t.Fatal("recursive ancestor not marked")
	}
	press(t, s, "j", "l")
	if tree.selected != cycle || cycle.open || len(cycle.children) != 0 {
		t.Fatal("cycle expanded")
	}
	cached := len(s.flow.expanded)
	press(t, s, "h", "h", "l")
	if len(s.flow.expanded) != cached {
		t.Fatal("reopening must reuse parsed results")
	}
}

func TestCallTreeUnavailableAndCancel(t *testing.T) {
	s := treeScreen(t)
	before := s.screenPosition
	press(t, s, "t")
	for _, row := range s.tree.rows() {
		if s.reason(row.node.site) == "" {
			continue
		}
		s.tree.selected = row.node
		count := len(s.flow.expanded)
		press(t, s, "l", "\r")
		if row.node.open || !s.tree.visible || s.screenPosition != before || len(s.flow.expanded) != count {
			t.Fatal("unavailable node changed code or expanded")
		}
		if !strings.Contains(s.renderTree(160, 24, false), s.reason(row.node.site)) {
			t.Fatal("missing reason")
		}
	}
	press(t, s, "esc")
	if s.tree.visible || s.screenPosition != before {
		t.Fatal("cancel changed source view")
	}
}

func TestCallTreeJumpHistoryAndReveal(t *testing.T) {
	s := treeScreen(t)
	before := s.screenPosition
	press(t, s, "t", "j", "l", "j", "\r") // main -> A -> C; jump directly to C
	if !strings.HasSuffix(s.current, ".C") || len(s.history) != 1 || s.tree.visible {
		t.Fatal("tree jump failed")
	}
	press(t, s, "t")
	if !strings.HasSuffix(s.tree.selected.site.To, ".C") || !s.tree.selected.parent.open {
		t.Fatal("reopening failed to reveal non-direct jump")
	}
	press(t, s, "esc", "b")
	if s.screenPosition != before {
		t.Fatal("back did not restore source position")
	}
	// Ordinary source navigation also reveals ancestors and siblings.
	press(t, s, "\r", "t")
	if s.tree.root.site.To != before.current || s.tree.selected.site.Name != "A" || !s.tree.root.open {
		t.Fatal("navigation ancestry not revealed")
	}
}

func TestCallTreeRenderResize(t *testing.T) {
	s := treeScreen(t)
	press(t, s, "t")
	s.tree.selected = s.tree.rows()[4].node
	position, selected := s.screenPosition, s.tree.selected
	positions := regexp.MustCompile("\x1b\\[[0-9;]*H")
	for _, size := range [][2]int{{41, 8}, {80, 12}, {120, 24}, {200, 48}} {
		for _, color := range []bool{false, true} {
			frame := s.renderTree(size[0], size[1], color)
			plain := sgrPattern.ReplaceAllString(frame, "")
			if !color && plain != frame {
				t.Fatal("NO_COLOR emitted styling")
			}
			for _, row := range positions.Split(plain, -1) {
				if row != "" && displayWidth(row) != min(100, size[0]-4) {
					t.Fatalf("bad popup width: %q", row)
				}
			}
			s.render(size[0], size[1], color)
			if s.screenPosition != position || s.tree.selected != selected {
				t.Fatal("render/resize changed source or selection")
			}
		}
	}
	if s.tree.top < 0 {
		t.Fatal("invalid scroll")
	}
	press(t, s, "esc")
	if s.screenPosition != position {
		t.Fatal("resize/cancel changed view")
	}
}

func TestCallTreeLoopEscapeAndModalKeys(t *testing.T) {
	s := treeScreen(t)
	s.agent = newAgentPane(agentOptions{excludes: []string{"main.go"}})
	before := s.screenPosition
	// Read timeout after Escape must close the popup, not quit or feed agent actions.
	input := []byte{'t', 'j', 'e', 'i', 0x1b, 0, 'q'}
	index := 0
	err := screenLoop(context.Background(), s, io.Discard, func() (byte, bool, error) {
		if index >= len(input) {
			return 0, false, fmt.Errorf("loop did not quit")
		}
		b := input[index]
		index++
		return b, b != 0, nil
	}, func() (int, int, error) { return 120, 24, nil }, false)
	if err != nil {
		t.Fatal(err)
	}
	if s.tree.visible || s.screenPosition != before {
		t.Fatal("Escape did not restore source view")
	}
	if s.agent.visible || s.agent.input != nil || len(s.agent.texts) != 0 {
		t.Fatal("popup keys reached the agent pane")
	}
}
