package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hib2018/HodoCode/internal/analyze"
)

var errOpenNeovim = errors.New("open Neovim outside terminal mode")

// Neovim's cursor() takes a byte column, not a screen-cell or rune column.
func (s *screen) neovimCommand(ctx context.Context) (*exec.Cmd, error) {
	source, err := s.p.SourcePosition(s.current)
	if err != nil {
		return nil, err
	}
	raw := []rune(s.rawLine(s.cursorRow))
	column := len(string(raw[:min(max(0, s.cursorColumn), len(raw))])) + 1
	if s.cursorRow == 0 {
		column += source.Column - 1
	}
	line := source.Line + s.cursorRow
	position := fmt.Sprintf("call cursor(%d,%d)", line, column)
	if s.nvimServer == "" {
		return exec.CommandContext(ctx, "nvim", "+"+position, "--", source.Filename), nil
	}
	// Use an expression rather than remote-send: no key notation is interpreted,
	// and fnameescape protects Ex metacharacters inside the filename.
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }
	expression := fmt.Sprintf("execute('edit ' . fnameescape(%s)) . cursor(%d,%d)", quote(source.Filename), line, column)
	return exec.CommandContext(ctx, "nvim", "--server", s.nvimServer, "--remote-expr", expression), nil
}

func (s *screen) reload(height, width int) error {
	p, err := analyze.Load(s.p.Root)
	if err != nil {
		return err
	}
	current := s.current
	if err := p.EnsureFunction(current); err != nil {
		current = ""
		for i := len(s.history) - 1; i >= 0; i-- {
			if p.EnsureFunction(s.history[i].current) == nil {
				current = s.history[i].current
				break
			}
		}
		if current == "" {
			ids := p.MainFunctions()
			if len(ids) == 0 {
				ids = functionIDs(p)
			}
			if len(ids) == 0 {
				return fmt.Errorf("復帰先の関数がありません")
			}
			current = ids[0]
		}
	}
	next, err := newScreen(p, current, nil)
	if err != nil {
		return err
	}
	for id := range s.flow.expanded {
		if id != current && p.EnsureFunction(id) == nil {
			next.flow.nodes[id] = true
			if err := next.flow.expand(id); err != nil {
				return err
			}
		}
	}
	for _, pos := range s.history {
		if p.EnsureFunction(pos.current) == nil {
			next.history = append(next.history, pos)
		}
	}
	if current == s.current {
		next.screenPosition = s.screenPosition
	}
	if err := next.refresh(); err != nil {
		return err
	}
	next.followCursor(height, width)
	next.nvimServer = s.nvimServer
	if s.agent != nil {
		if s.agent.cancel != nil {
			s.agent.cancel()
		}
		next.agent = newAgentPane(s.agent.options)
	}
	next.notice = "再読み込みしました"
	if current != s.current {
		next.notice += "。表示中の関数がなくなったため " + current + " へ移動しました"
	}
	*s = *next
	return nil
}
