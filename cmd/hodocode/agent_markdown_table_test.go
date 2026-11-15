package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownCellsBacktickRuns(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"double", "| ``a|b`` | next |", []string{"``a|b``", "next"}},
		{"triple", "| ```a|b``` | next |", []string{"```a|b```", "next"}},
		{"unequal inner runs", "| ``a`|b```|c`` | next |", []string{"``a`|b```|c``", "next"}},
		{"literal backslash in code", "| ``a\\`` | next |", []string{"``a\\``", "next"}},
		{"escaped pipe", "| a\\|b | next |", []string{"a\\|b", "next"}},
		{"escaped backtick", "| \\`a | next |", []string{"\\`a", "next"}},
		{"unicode", "| ``日本|語`` | 次 |", []string{"``日本|語``", "次"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdownCells(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("markdownCells(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestMarkdownTableStopsBeforeFence(t *testing.T) {
	for _, fence := range []string{"```sh|example", "~~~sh|example", "  ````sh|example", "  ~~~~sh|example"} {
		t.Run(fence, func(t *testing.T) {
			raw := []string{"| Name | Value |", "| --- | --- |", "| item | value |", fence, "**literal**", fence[:strings.Index(fence, "sh")], "plain"}
			for _, width := range []int{16, 80} {
				var out markdownOutput
				if used := markdownTable(raw, width, &out); used != 3 {
					t.Fatalf("width %d consumed %d lines, want 3", width, used)
				}
				lines, _ := markdownLines(strings.Join(raw, "\n"), width)
				joined := strings.Join(lines, "\n")
				if !strings.Contains(joined, "│ **literal**") || !strings.Contains(joined, "└─\nplain") {
					t.Fatalf("width %d lost fenced code after table: %q", width, lines)
				}
			}
		})
	}
}

func TestMarkdownTableMultiBacktickCell(t *testing.T) {
	raw := []string{"| Code | Value |", "| --- | --- |", "| ``a|b`` | next |"}
	for _, width := range []int{12, 80} {
		var out markdownOutput
		if used := markdownTable(raw, width, &out); used != len(raw) {
			t.Fatalf("width %d consumed %d lines, want %d", width, used, len(raw))
		}
		compact := strings.Join(strings.Fields(strings.Join(out.lines, "")), "")
		if !strings.Contains(compact, "`a|b`") || !strings.Contains(compact, "next") || strings.Contains(compact, "列3") {
			t.Fatalf("width %d split code cell: %q", width, out.lines)
		}
	}
}
