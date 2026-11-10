package main

import (
	"strings"
	"testing"
)

func TestSourceTopics(t *testing.T) {
	for _, tt := range []struct{ source, needle, topic string }{
		{"func f(p *int) { _ = *p; _ = 2 * 3; _ = &p; _ = 3 & 2 }", "*int", "pointer_type"},
		{"func f(p *int) { _ = *p }", "*p", "dereference"},
		{"func f() { _ = (*int)(nil) }", "*int", "pointer_type"},
		{"func f() { p := new(func()); (*p)() }", "*p", "dereference"},
		{"func f() { _ = 2 * 3 }", "*", "multiply"},
		{"func f() { v := 2; _ = &v }", "&v", "address_of"},
		{"func f() { _ = 3 & 2 }", "&", "bitwise_and"},
		{"func f(ch chan int) { ch <- 1 }", "<-", "send"},
		{"func f(ch chan int) { _ = <-ch }", "<-", "receive"},
		{"func f(ch chan<- int) {}", "<-", "chan_send"},
		{"func f(ch <-chan int) {}", "<-", "chan_receive"},
		{"func f() { for i := 0; i < 3; i++ {} }", "for", "for_clause"},
		{"func f() { for true {} }", "for", "for_condition"},
		{"func f() { for {} }", "for", "for_infinite"},
		{"func f() { for range 3 {} }", "for", "range_loop"},
		{"func f() { switch 1 { case 1: } }", "switch", "expression_switch"},
		{"func f(v any) { switch v.(type) { case int: } }", "switch", "type_switch"},
		{"func f(v any) { switch v.( /* type in comment */ type) { case int: } }", "type)", "type_switch"},
		{"func f() { select { case <-make(chan int): } }", "case", "select_case"},
		{"func f() { switch 1 { case 1: } }", "case", "switch_case"},
		{"func f() {}", "func", "func_decl"},
		{"type T int; func (T) f() {}", "func", "method_decl"},
		{"var f = func() {}", "func", "func_literal"},
		{"var f func()", "func", "func_type"},
		{"var a []int", "[", "slice_type"},
		{"var a [2]int", "[", "array_type"},
		{"func f() { _ = make([]int, 2) }", "[", "slice_type"},
		{"func f(a []int) { _ = a[0] }", "[0]", "index"},
		{"func f(a []int) { _ = a[1:2] }", "[1:2]", "slice"},
		{"func f(a []int) { _ = a[1:2:3] }", "[1:2:3]", "full_slice"},
		{"var a = []int{1}", "{", "composite_literal"},
		{"func f(a any) { _ = a.(int) }", "(int)", "type_assertion"},
		{"type A = int", "=", "type_alias"},
		{"func f(a ...int) {}", "...", "variadic_param"},
		{"func f(a []int) { println(a...) }", "...", "argument_spread"},
		{"var a = [...]int{1}", "...", "array_length"},
		{"var a = `text`", "`text`", "raw_string"},
		{"var a = \"text\"", "\"text\"", "interpreted_string"},
		{"func f(a []int) { _ = len(a) }", "len", "builtin_len"},
		{"func f() { var a int; _ = a }", "int", "type_int"},
		{"type B[T any] struct{}; var b B[int]", "[int]", "type_arguments"},
	} {
		t.Run(tt.topic+tt.needle, func(t *testing.T) {
			offset := strings.Index(tt.source, tt.needle)
			if offset < 0 {
				t.Fatal("missing fixture needle")
			}
			topics := sourceTopics(tt.source)
			if got := topics[offset]; got != tt.topic {
				t.Fatalf("at %q got %q, want %q", tt.needle, got, tt.topic)
			}
			if syntaxTopics[tt.topic].reference == "" {
				t.Fatal("topic lacks help")
			}
			found := false
			for _, note := range sourceExplanations(tt.source) {
				if note.topic == tt.topic {
					found = true
				}
			}
			if !found {
				t.Fatal("context did not reach token explanations")
			}
		})
	}
}

func TestSourceTopicsShadowingAndFallback(t *testing.T) {
	for _, tt := range []struct{ source, blocked string }{
		{"func f() { len := func([]int) int { return 0 }; _ = len(nil) }", "builtin_len"},
		{"func len([]int) int { return 0 }; func f() { _ = len(nil) }", "builtin_len"},
		{"func f() { type int string; var a int; _ = a }", "type_int"},
	} {
		for _, topic := range sourceTopics(tt.source) {
			if topic == tt.blocked {
				t.Fatalf("shadowed name misclassified in %q", tt.source)
			}
		}
	}
	for _, source := range []string{"func broken(", "p := &value", ""} {
		if len(sourceTopics(source)) != 0 {
			t.Fatalf("invalid snippet should use token help: %q", source)
		}
	}
	// Physical byte offsets must ignore //line directives and keep Unicode columns.
	source := "//line other.go:500\nfunc 日本語(p *int) { _ = *p }"
	for _, note := range sourceExplanations(source) {
		if note.topic == "pointer_type" && (note.row != 1 || note.column != 11) {
			t.Fatalf("wrong physical/rune position: %+v", note)
		}
	}
}
