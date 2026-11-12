package main

import (
	"fmt"
	"strings"

	"github.com/hib2018/HodoCode/internal/analyze"
)

type callTreeNode struct {
	site                analyze.CallSite
	parent              *callTreeNode
	children            []*callTreeNode
	loaded, open, cycle bool
}

type callTree struct {
	root, selected *callTreeNode
	visible        bool
	top            int
}

func (s *screen) expandTree(n *callTreeNode) error {
	if n.cycle || s.reason(n.site) != "" {
		return nil
	}
	if !n.loaded {
		if err := s.flow.expand(n.site.To); err != nil {
			return err
		}
		seen := map[analyze.Edge]bool{}
		for _, site := range s.flow.sites[n.site.To] {
			if seen[site.Edge] {
				continue
			}
			seen[site.Edge] = true
			child := &callTreeNode{site: site, parent: n}
			for ancestor := n; ancestor != nil; ancestor = ancestor.parent {
				if !site.Unknown && ancestor.site.To == site.To {
					child.cycle = true
					break
				}
			}
			n.children = append(n.children, child)
		}
		n.loaded = true
	}
	n.open = true
	return nil
}

func (s *screen) openTree() error {
	root := s.current
	if len(s.history) > 0 {
		root = s.history[0].current
	}
	if s.tree == nil || s.tree.root.site.To != root {
		n := &callTreeNode{site: analyze.CallSite{Edge: analyze.Edge{To: root}}}
		s.tree = &callTree{root: n, selected: n}
	}
	t := s.tree
	n := t.root
	// Reveal the navigation path without closing branches the user already opened.
	path := append(append([]screenPosition(nil), s.history...), s.screenPosition)
	for i, position := range path {
		if i == 0 {
			continue
		}
		if err := s.expandTree(n); err != nil {
			return err
		}
		var next *callTreeNode
		for _, child := range n.children {
			if !child.site.Unknown && child.site.To == position.current {
				next = child
				break
			}
		}
		if next == nil {
			break
		}
		n = next
	}
	if n.site.To != s.current {
		var find func(*callTreeNode) *callTreeNode
		find = func(node *callTreeNode) *callTreeNode {
			if !node.site.Unknown && node.site.To == s.current {
				return node
			}
			for _, child := range node.children {
				if found := find(child); found != nil {
					return found
				}
			}
			return nil
		}
		if found := find(t.root); found != nil {
			n = found
		}
	}
	for ancestor := n.parent; ancestor != nil; ancestor = ancestor.parent {
		ancestor.open = true
	}
	if err := s.expandTree(n); err != nil {
		return err
	}
	t.selected, t.visible = n, true
	return nil
}

type callTreeRow struct {
	node   *callTreeNode
	prefix string
}

func (t *callTree) rows() []callTreeRow {
	var rows []callTreeRow
	var visit func(*callTreeNode, string, string)
	visit = func(n *callTreeNode, prefix, branch string) {
		rows = append(rows, callTreeRow{n, prefix + branch})
		if !n.open {
			return
		}
		if n.parent != nil {
			if branch == "└─ " {
				prefix += "   "
			} else {
				prefix += "│  "
			}
		}
		for i, child := range n.children {
			branch := "├─ "
			if i == len(n.children)-1 {
				branch = "└─ "
			}
			visit(child, prefix, branch)
		}
	}
	visit(t.root, "", "")
	return rows
}

func (s *screen) treeKey(key string, height, codeWidth int) (bool, error) {
	t := s.tree
	rows := t.rows()
	index := 0
	for i, row := range rows {
		if row.node == t.selected {
			index = i
			break
		}
	}
	n := t.selected
	switch key {
	case "\x03", "\x04":
		return true, nil
	case "esc", "\x1b", "q", "t":
		t.visible = false
	case "up", "k":
		t.selected = rows[max(0, index-1)].node
	case "down", "j":
		t.selected = rows[min(len(rows)-1, index+1)].node
	case "pageup":
		t.selected = rows[max(0, index-max(1, height-5))].node
	case "pagedown":
		t.selected = rows[min(len(rows)-1, index+max(1, height-5))].node
	case "right", "l":
		if n.open && len(n.children) > 0 {
			t.selected = n.children[0]
		} else if err := s.expandTree(n); err != nil {
			return false, err
		}
	case "left", "h":
		if n.open {
			n.open = false
		} else if n.parent != nil {
			t.selected = n.parent
		}
	case "\r", "\n":
		if s.reason(n.site) != "" {
			return false, nil
		}
		if err := s.flow.expand(n.site.To); err != nil {
			return false, err
		}
		previous := s.screenPosition
		s.screenPosition = screenPosition{current: n.site.To}
		if err := s.refresh(); err != nil {
			s.screenPosition = previous
			return false, err
		}
		s.history = append(s.history, previous)
		s.jumpCall()
		s.followCursor(height, codeWidth)
		t.visible = false
	}
	return false, nil
}

func (s *screen) renderTree(width, height int, color bool) string {
	if s.tree == nil || !s.tree.visible || width < 41 || height < 8 {
		return ""
	}
	t := s.tree
	boxWidth, boxHeight := min(100, width-4), min(26, height-2)
	inner, body := boxWidth-2, boxHeight-5
	left, top := (width-boxWidth)/2+1, (height-boxHeight)/2+1
	rows := t.rows()
	selected := 0
	for i, row := range rows {
		if row.node == t.selected {
			selected = i
			break
		}
	}
	if selected < t.top {
		t.top = selected
	}
	if selected >= t.top+body {
		t.top = selected - body + 1
	}
	t.top = max(0, min(t.top, max(0, len(rows)-body)))
	var b strings.Builder
	write := func(row int, text string, active bool) {
		fmt.Fprintf(&b, "\x1b[%d;%dH", top+row, left)
		style := "38;5;252;48;5;237"
		if active {
			style = "1;38;5;110;48;5;238"
		}
		b.WriteString(uiPaint(text, style, color))
	}
	border := "+" + strings.Repeat("-", inner) + "+"
	write(0, border, false)
	write(1, "│"+displaySlice(" 呼び出しツリー  j/k 選択 · h/l 開閉 · Enter 移動 · Esc 閉じる", 0, inner)+"│", false)
	for i := 0; i < body; i++ {
		text := ""
		active := false
		if t.top+i < len(rows) {
			row := rows[t.top+i]
			n := row.node
			active = n == t.selected
			marker := "+ "
			switch {
			case n.cycle:
				marker = "↻ "
			case s.reason(n.site) != "":
				marker = "× "
			case n.open:
				marker = "- "
			case n.loaded && len(n.children) == 0:
				marker = "· "
			}
			name := n.site.Name
			if name == "" {
				name = n.site.To
				if slash := strings.LastIndex(name, "/"); slash >= 0 {
					name = name[slash+1:]
				}
			}
			cursor := "  "
			if active {
				cursor = "> "
			}
			text = cursor + row.prefix + marker + name
			if n.cycle {
				text += " (循環参照)"
			} else if reason := s.reason(n.site); reason != "" {
				text += " (" + reason + ")"
			}
		}
		write(i+2, "│"+displaySlice(text, 0, inner)+"│", active)
	}
	detail := t.selected.site.To
	if fn, ok := s.p.Functions[detail]; ok && !t.selected.site.Unknown {
		detail += fmt.Sprintf("  %s:%d", fn.File, fn.Line)
	}
	write(body+2, "│"+displaySlice(detail, 0, inner)+"│", false)
	write(body+3, "│"+displaySlice(fmt.Sprintf(" %d/%d", selected+1, len(rows)), 0, inner)+"│", false)
	write(body+4, border, false)
	return b.String()
}
