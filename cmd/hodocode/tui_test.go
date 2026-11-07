package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestTreeLabels(t *testing.T) {
	current := "example.com/app/pkg.Run"
	ids := []string{current, "example.com/app/pkg.Worker.Step", "example.com/lib/analyze.Load", "example.com/one/helper.Work", "example.com/two/helper.Work", "example.com/three/helper.Work"}
	ids = append(ids, ids[3]) // The parent or recursive callee may appear twice.
	labels := treeLabels(current, ids)
	for id, want := range map[string]string{
		current: "Run",
		ids[1]:  "Worker.Step",
		ids[2]:  "analyze.Load",
		ids[3]:  ids[3], ids[4]: ids[4], ids[5]: ids[5],
	} {
		if labels[id] != want {
			t.Errorf("label(%q) = %q, want %q", id, labels[id], want)
		}
	}
}

func TestTUISingleChoiceEnter(t *testing.T) {
	for _, input := range []string{"q\n", "\n\nb\nq\n"} {
		var out bytes.Buffer
		err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "CycleA"}, strings.NewReader(input), &out, io.Discard, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if !strings.Contains(got, "Enterで CycleB へ進む") {
			t.Fatal(got)
		}
		if input == "q\n" {
			if strings.Contains(got, "▶ CycleB（現在）") {
				t.Fatal("moved without Enter")
			}
		} else {
			if strings.Count(got, "▶ CycleB（現在）") != 2 || !strings.Contains(got, "CycleB\n└─ ▶ CycleA（現在）") {
				t.Fatal(got)
			}
		}
	}
}

func TestTUIDetailsToggle(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"tui", "--root", fixture(t), "--func", "helper.Work"}, strings.NewReader("d\nd\nq\n"), &out, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Count(got, "外部ソース: 1件") != 2 || strings.Count(got, "└─ strings.TrimSpace（外部ソース・選択不可）") != 1 || strings.Count(got, "▶ Work（現在）") != 3 {
		t.Fatal(got)
	}
	if strings.Contains(got, "1: strings.TrimSpace") {
		t.Fatal("external call became selectable")
	}
}
