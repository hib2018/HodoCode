package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestConversationSnapshotAndPrompts(t *testing.T) {
	base := prepare(t, fixture(t), nil)
	history := []Message{{Role: "user", Content: "前の質問 <untrusted>"}, {Role: "assistant", Content: "前の回答"}}
	want := append([]Message(nil), history...)
	next, err := base.WithConversation(history, "その理由は？")
	if err != nil {
		t.Fatal(err)
	}
	history[0].Content = "changed"
	base.candidates[0].File = "changed.go"
	base.snippets[next.target] = "changed source"
	base.graph = append(base.graph, link{From: "changed"})
	if next.candidates[0].File == "changed.go" || next.snippets[next.target] == "changed source" || len(next.graph) != 0 {
		t.Fatal("snapshot aliases base")
	}
	if base.question != "" || len(base.history) != 0 {
		t.Fatal("base conversation changed")
	}
	calls := 0
	answer, err := next.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		var data struct {
			Purpose  string    `json:"purpose"`
			History  []Message `json:"history"`
			Question string    `json:"question"`
		}
		parts := strings.SplitN(prompt, "UNTRUSTED DATA:\n", 2)
		if len(parts) != 2 {
			t.Fatal("missing JSON")
		}
		if err := json.Unmarshal([]byte(parts[1]), &data); err != nil {
			t.Fatal(err)
		}
		if data.Purpose != base.purpose || data.Question != "その理由は？" || !reflect.DeepEqual(data.History, want) {
			t.Fatalf("wrong conversation: %+v", data)
		}
		if !strings.Contains(prompt, "history, question") || !strings.Contains(prompt, "Do not use tools") {
			t.Fatal("missing trust boundary")
		}
		if calls == 1 {
			return `["` + next.target + `"]`, nil
		}
		for _, instruction := range []string{"日本語のみ", "現在の question にだけ", "明示的に求められない限り", "file:line"} {
			if !strings.Contains(prompt, instruction) {
				t.Errorf("missing %q", instruction)
			}
		}
		return "理由", nil
	})
	if err != nil || calls != 2 || answer != "理由" {
		t.Fatalf("answer=%q err=%v calls=%d", answer, err, calls)
	}
	// A second follow-up replaces history without changing the first.
	if _, err := next.WithConversation(nil, "次の質問"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.history, want) {
		t.Fatal("follow-up mutated original")
	}
}

func TestConversationValidation(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	for _, question := range []string{"", " \n\t", strings.Repeat("x", maxPurpose+1)} {
		if _, err := r.WithConversation(nil, question); err == nil {
			t.Errorf("accepted question length %d", len(question))
		}
	}
	for _, message := range []Message{{"system", "x"}, {"tool", "x"}, {"User", "x"}, {"", "x"}, {"user", ""}, {"assistant", " \n"}} {
		if _, err := r.WithConversation([]Message{message}, "question"); err == nil {
			t.Errorf("accepted %+v", message)
		}
	}
	for _, invalid := range []*Request{nil, {}} {
		if _, err := invalid.WithConversation(nil, "question"); err == nil {
			t.Fatal("accepted unprepared request")
		}
	}
	if _, err := r.WithConversation(nil, strings.Repeat("x", maxPurpose)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WithConversation([]Message{{"assistant", strings.Repeat("x", maxPrompt)}}, "question"); err == nil {
		t.Fatal("accepted oversized history")
	}
}

func TestConversationOverflowBeforeAnyCalls(t *testing.T) {
	for _, mode := range []string{"selection", "answer", "graph"} {
		t.Run(mode, func(t *testing.T) {
			r := prepare(t, fixture(t), nil)
			if mode != "selection" {
				// The largest sources need not include the target. JSON escaping, not raw
				// source length, determines the bound; the 13th source must be excluded.
				for i := 0; i < 13; i++ {
					id := fmt.Sprintf("p.F%d", i)
					r.candidates = append(r.candidates, candidate{ID: id, File: "f.go", Line: 1, End: 1})
					r.snippets[id] = strings.Repeat("<", 1500)
				}
				if mode == "graph" {
					for i := 0; i < 100; i++ {
						r.graph = append(r.graph, link{From: r.target, To: strings.Repeat("g", 200), Unknown: true})
					}
				}
			}
			// This fits selection for answer/graph, but not their worst-case answer.
			history := []Message{{"assistant", strings.Repeat("h", 25*1024)}}
			if mode == "graph" {
				history[0].Content = strings.Repeat("h", 10*1024)
				graph := r.graph
				r.graph = nil
				if _, err := r.WithConversation(history, "question"); err != nil {
					t.Fatalf("without graph: %v", err)
				}
				r.graph = graph
			}
			if mode == "selection" {
				history[0].Content = strings.Repeat("h", maxPrompt)
			}
			if _, err := r.WithConversation(history, "question"); err == nil {
				t.Fatal("accepted overflow")
			}
			// Explain also preflights independently before invoking a runner.
			r.history, r.question = history, "question"
			calls := 0
			_, err := r.Explain(context.Background(), func(context.Context, string) (string, error) { calls++; return `["` + r.target + `"]`, nil })
			if err == nil || calls != 0 || !strings.Contains(err.Error(), "prompt exceeds") {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestPreflightLimitsSourcesToTwelve(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("p.F%d", i)
		r.candidates = append(r.candidates, candidate{ID: id, File: "f.go", Line: 1, End: 1})
		r.snippets[id] = strings.Repeat("x", 7000)
	}
	if _, err := r.WithConversation(nil, "question"); err != nil {
		t.Fatal(err)
	}
}
