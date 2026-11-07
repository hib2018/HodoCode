package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/hib2018/HodoCode/internal/analyze"
	"github.com/hib2018/HodoCode/internal/explain"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, explain.FX); err != nil {
		fmt.Fprintln(os.Stderr, "hodocode:", err)
		os.Exit(1)
	}
}

type list []string

func (s *list) String() string     { return strings.Join(*s, ",") }
func (s *list) Set(v string) error { *s = append(*s, v); return nil }

func run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer, runner explain.Runner) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(out, "Usage: hodocode <deps|functions|flow|tui|explain> [flags]\nCommon: --root PATH (repository or Go module root; includes nested modules)\ndeps: --dir . --depth -1 (source directory depth)\nflow/tui: --func NAME [--expand NAME ...]\nexplain: --func NAME --purpose TEXT [--exclude PATTERN ...] [--send] [--timeout 2m]\nexplain defaults to a local preview; --send invokes fx twice. Run a command with --help for flags.")
		return err
	}
	command := args[0]
	switch command {
	case "deps", "functions", "flow", "tui", "explain":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository or Go module root; includes nested modules")
	var dir, name, purpose *string
	var depth *int
	var send *bool
	var timeout *time.Duration
	var expansions, excludes list
	if command == "deps" {
		dir = fs.String("dir", ".", "source directory relative to root")
		depth = fs.Int("depth", -1, "source directory depth; 0 = directory only, -1 = unlimited")
	}
	if command == "flow" || command == "tui" || command == "explain" {
		name = fs.String("func", "", "exact function ID or unique suffix")
	}
	if command == "flow" || command == "tui" {
		fs.Var(&expansions, "expand", "reachable function to expand one level (repeatable)")
	}
	if command == "explain" {
		purpose = fs.String("purpose", "", "analysis purpose (sent verbatim; do not include secrets)")
		fs.Var(&excludes, "exclude", "relative filepath.Match pattern or directory prefix (repeatable; no **)")
		send = fs.Bool("send", false, "send metadata and sanitized selected snippets to fx after auditing preview")
		timeout = fs.Duration("timeout", 2*time.Minute, "total timeout for both fx requests")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	p, err := analyze.Load(*root)
	if err != nil {
		return err
	}
	switch command {
	case "functions":
		for _, id := range functionIDs(p) {
			f := p.Functions[id]
			if _, err := fmt.Fprintf(out, "%s\t%s:%d-%d\n", id, f.File, f.Line, f.End); err != nil {
				return err
			}
		}
		return nil
	case "deps":
		edges, err := p.Dependencies(*dir, *depth)
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, analyze.Mermaid(edges))
		return err
	case "tui":
		return interactive(p, *name, expansions, in, out)
	case "flow":
		s, err := newFlow(p, *name, expansions)
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, s.diagram())
		return err
	case "explain":
		if *timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		id, err := p.Resolve(*name)
		if err != nil {
			return err
		}
		edges, err := p.Calls(id)
		if err != nil {
			return err
		}
		req, err := explain.Prepare(p, id, *purpose, edges, excludes)
		if err != nil {
			return err
		}
		if !*send {
			_, err = fmt.Fprintln(out, req.Preview())
			return err
		}
		if _, err = fmt.Fprintln(stderr, req.Preview()); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		text, err := req.Explain(ctx, runner)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, text)
		return err
	}
	return nil
}

func functionIDs(p *analyze.Project) []string {
	ids := make([]string, 0, len(p.Functions))
	for id := range p.Functions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type flow struct {
	p        *analyze.Project
	edges    []analyze.Edge
	nodes    map[string]bool
	expanded map[string]bool
}

func newFlow(p *analyze.Project, name string, expansions []string) (*flow, error) {
	id, err := p.Resolve(name)
	if err != nil {
		return nil, err
	}
	s := &flow{p: p, nodes: map[string]bool{id: true}, expanded: map[string]bool{}}
	if err = s.expand(id); err != nil {
		return nil, err
	}
	for _, name := range expansions {
		id, err = p.Resolve(name)
		if err != nil {
			return nil, err
		}
		if err = s.expand(id); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *flow) expand(id string) error {
	if !s.nodes[id] {
		return fmt.Errorf("%s is not a reachable function; expand its caller first", id)
	}
	if s.expanded[id] {
		return nil
	}
	edges, err := s.p.Calls(id)
	if err != nil {
		return err
	}
	s.edges = append(s.edges, edges...)
	s.expanded[id] = true
	for _, e := range edges {
		if !e.Unknown {
			if _, ok := s.p.Functions[e.To]; ok {
				s.nodes[e.To] = true
			}
		}
	}
	return nil
}
func (s *flow) diagram() string {
	nodes := make([]string, 0, len(s.nodes))
	for id := range s.nodes {
		nodes = append(nodes, id)
	}
	return analyze.MermaidNodes(s.edges, nodes)
}
func choose(scan *bufio.Scanner, out io.Writer, ids []string, back bool, labels map[string]string, details bool) (string, error) {
	prompt := "番号で選択、qで終了 > "
	if back {
		prompt = "番号で選択、bで戻る、qで終了 > "
		if len(ids) == 0 {
			prompt = "bで戻る、qで終了 > "
		}
	}
	if len(ids) == 0 && !back {
		prompt = "qで終了 > "
	}
	if len(ids) == 1 && labels != nil {
		prompt = "Enterで " + labels[ids[0]] + " へ進む、" + prompt
	}
	if details {
		prompt = "dで詳細切替、" + prompt
	}
	for {
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return "", err
		}
		if !scan.Scan() {
			return "", scan.Err()
		}
		text := strings.TrimSpace(scan.Text())
		if text == "q" {
			return "", nil
		}
		if back && text == "b" {
			return "b", nil
		}
		if details && text == "d" {
			return "d", nil
		}
		if text == "" && len(ids) == 1 && labels != nil {
			return ids[0], nil
		}
		var n int
		// Parse the entire input, not a numeric prefix.
		if _, err := fmt.Sscan(text, &n); err == nil && fmt.Sprint(n) == text && n >= 1 && n <= len(ids) {
			return ids[n-1], nil
		}
		if _, err := fmt.Fprintln(out, "表示された番号を入力してください。"); err != nil {
			return "", err
		}
	}
}
func currentLabel(id string, bold bool) string {
	label := "▶ " + id + "（現在）"
	if bold {
		return "\x1b[1m" + label + "\x1b[0m"
	}
	return label
}

func treeLabels(current string, ids []string) map[string]string {
	split := func(id string) (string, string) {
		start := strings.LastIndex(id, "/") + 1
		if dot := strings.Index(id[start:], "."); dot >= 0 {
			return id[:start+dot], id[start+dot+1:]
		}
		return "", id
	}
	currentPackage, _ := split(current)
	labels := make(map[string]string, len(ids))
	owners := map[string]string{}
	for _, id := range ids {
		pkg, name := split(id)
		label := name
		if pkg != currentPackage {
			label = pkg[strings.LastIndex(pkg, "/")+1:] + "." + name
		}
		labels[id] = label
		if previous, ok := owners[label]; ok && previous != id {
			owners[label] = ""
		} else {
			owners[label] = id
		}
	}
	for id, label := range labels {
		if owners[label] == "" {
			labels[id] = id
		}
	}
	return labels
}

func interactive(p *analyze.Project, name string, expansions []string, in io.Reader, out io.Writer) error {
	bold := false
	if file, ok := out.(*os.File); ok && os.Getenv("TERM") != "dumb" {
		_, noColor := os.LookupEnv("NO_COLOR")
		info, err := file.Stat()
		bold = !noColor && err == nil && info.Mode()&os.ModeCharDevice != 0
	}
	scan := bufio.NewScanner(in)
	if name == "" {
		ids := functionIDs(p)
		if len(ids) == 0 {
			return fmt.Errorf("no functions found")
		}
		for i, id := range ids {
			if _, err := fmt.Fprintf(out, "%d: %s\n", i+1, id); err != nil {
				return err
			}
		}
		var err error
		name, err = choose(scan, out, ids, false, nil, false)
		if err != nil {
			return err
		}
		if name == "" {
			return nil
		}
	}
	s, err := newFlow(p, name, expansions)
	if err != nil {
		return err
	}
	current, err := p.Resolve(name)
	if err != nil {
		return err
	}
	var history []string
	showDetails := false
	for {
		var children []analyze.Edge
		visible := []string{current}
		if len(history) > 0 {
			visible = append(visible, history[len(history)-1])
		}
		external, unknown := 0, 0
		for _, edge := range s.edges {
			if edge.From != current {
				continue
			}
			_, local := p.Functions[edge.To]
			if edge.Unknown {
				unknown++
			} else if !local {
				external++
			}
			if !edge.Unknown {
				visible = append(visible, edge.To)
			}
			if showDetails || (!edge.Unknown && local) {
				children = append(children, edge)
			}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].To < children[j].To })
		labels := treeLabels(current, visible)
		if _, err = fmt.Fprintln(out); err != nil {
			return err
		}
		indent := ""
		if len(history) > 0 {
			if _, err = fmt.Fprintln(out, labels[history[len(history)-1]]); err != nil {
				return err
			}
			indent = "   "
		}
		prefix := ""
		if len(history) > 0 {
			prefix = "└─ "
		}
		if _, err = fmt.Fprintf(out, "%s%s\n", prefix, currentLabel(labels[current], bold)); err != nil {
			return err
		}
		var ids []string
		for i, edge := range children {
			branch := "├─ "
			if i == len(children)-1 {
				branch = "└─ "
			}
			label := labels[edge.To]
			_, local := p.Functions[edge.To]
			switch {
			case edge.Unknown:
				label = edge.To + "（呼び出し先不明・選択不可）"
			case !local:
				label += "（外部ソース・選択不可）"
			default:
				ids = append(ids, edge.To)
				label = fmt.Sprintf("%d: %s", len(ids), label)
			}
			if _, err = fmt.Fprintf(out, "%s%s%s\n", indent, branch, label); err != nil {
				return err
			}
		}
		hasDetails := external+unknown > 0
		if hasDetails && !showDetails {
			if _, err = fmt.Fprintf(out, "%s   外部ソース: %d件、呼び出し先不明: %d件（選択不可・dで詳細表示）\n", indent, external, unknown); err != nil {
				return err
			}
		}
		code, err := p.Source(current)
		if err != nil {
			return err
		}
		fn := p.Functions[current]
		if _, err = fmt.Fprintf(out, "\n%s:%d-%d\n%s\n%s\n%s\n", fn.File, fn.Line, fn.End, strings.Repeat("─", 60), code, strings.Repeat("─", 60)); err != nil {
			return err
		}
		if len(ids) == 0 && len(history) == 0 && !hasDetails {
			return nil
		}
		id, err := choose(scan, out, ids, len(history) > 0, labels, hasDetails)
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		if id == "d" {
			showDetails = !showDetails
			continue
		}
		if id == "b" {
			current = history[len(history)-1]
			history = history[:len(history)-1]
			continue
		}
		if err = s.expand(id); err != nil {
			return err
		}
		history = append(history, current)
		current = id
	}
}
