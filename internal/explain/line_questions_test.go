package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func batchQuestions() []LineQuestion {
	return []LineQuestion{
		{ID: "first", Function: "example.org/demo.First", File: "main.go", Line: 3, Text: "この行は？"},
		{ID: "second", Function: "example.org/demo.Second", File: "main.go", Line: 6, Text: "次は？"},
	}
}

func TestLineQuestionsSnapshotAndRequiredSources(t *testing.T) {
	p := fixture(t)
	base := prepare(t, p, nil)
	questions := batchQuestions()
	want := append([]LineQuestion(nil), questions...)
	batch, err := base.WithLineQuestions(questions)
	if err != nil {
		t.Fatal(err)
	}
	questions[0].Text = "changed"
	base.snippets[want[1].Function] = "changed source"
	if len(base.lineQuestions) != 0 || !reflect.DeepEqual(batch.lineQuestions, want) {
		t.Fatal("snapshot aliased")
	}
	if err := os.RemoveAll(p.Root); err != nil {
		t.Fatal(err)
	}
	calls := 0
	answer, err := batch.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		_, raw, ok := strings.Cut(prompt, "UNTRUSTED DATA:\n")
		var data struct {
			Questions []LineQuestion `json:"line_questions"`
			Sources   []source       `json:"sources"`
		}
		if !ok || json.Unmarshal([]byte(raw), &data) != nil || !reflect.DeepEqual(data.Questions, want) {
			t.Fatalf("missing questions: %s", prompt)
		}
		for _, bad := range []string{"PASSWORD_123", "COMMENT_SECRET", "OTHER_SECRET", "changed source"} {
			if strings.Contains(prompt, bad) {
				t.Errorf("leaked %s", bad)
			}
		}
		if calls == 1 {
			if len(data.Sources) != 0 {
				t.Fatal("selection included sources")
			}
			return `["example.org/demo.First"]`, nil
		}
		if len(data.Sources) != 2 || data.Sources[1].ID != want[1].Function || !strings.Contains(data.Sources[1].Snippet, "func Second") {
			t.Fatalf("missing required source: %+v", data.Sources)
		}
		if !strings.Contains(prompt, `{"answers":[`) || strings.Contains(prompt, `{"summary":`) || !strings.Contains(prompt, "line_questions, graph") {
			t.Fatal("wrong schema or trust boundary")
		}
		return `{"answers":[{"id":"second","text":"次の回答"},{"id":"first","text":"最初の回答"}]}`, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	got, err := ParseLineAnswers(answer, want)
	if err != nil || got["second"] != "次の回答" {
		t.Fatalf("%v %v", got, err)
	}
	// Explicit output-mode changes reset the batch, while batch replaces both modes.
	conversation, err := batch.WithConversation(nil, "why?")
	if err != nil || len(conversation.lineQuestions) != 0 {
		t.Fatalf("conversation: %v", err)
	}
	comments, err := batch.WithLineComments()
	if err != nil || len(comments.lineQuestions) != 0 {
		t.Fatalf("comments: %v", err)
	}
	again, err := comments.WithLineQuestions(want)
	if err != nil || again.lineComments || again.question != "" || len(again.history) != 0 {
		t.Fatalf("batch: %v", err)
	}
}

func TestLineQuestionsValidation(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	for _, invalid := range []*Request{nil, {}} {
		if _, err := invalid.WithLineQuestions(batchQuestions()); err == nil {
			t.Fatal("accepted unprepared request")
		}
	}
	if _, err := r.WithLineQuestions(nil); err == nil {
		t.Fatal("accepted no questions")
	}
	for _, tc := range []struct {
		name   string
		change func(*LineQuestion)
	}{
		{"empty ID", func(q *LineQuestion) { q.ID = " " }},
		{"large ID", func(q *LineQuestion) { q.ID = strings.Repeat("i", maxPurpose+1) }},
		{"bad UTF8 ID", func(q *LineQuestion) { q.ID = "\xff" }},
		{"empty text", func(q *LineQuestion) { q.Text = "\n " }},
		{"large text", func(q *LineQuestion) { q.Text = strings.Repeat("t", maxPurpose+1) }},
		{"bad UTF8 text", func(q *LineQuestion) { q.Text = "\xff" }},
		{"unknown", func(q *LineQuestion) { q.Function = "missing.F" }},
		{"nonlocal", func(q *LineQuestion) { q.Function = "fmt.Println" }},
		{"blocked", func(q *LineQuestion) { q.Function = "example.org/demo.Credentials" }},
		{"excluded", func(q *LineQuestion) { q.Function = "example.org/demo/skip.Worker" }},
		{"file", func(q *LineQuestion) { q.File = "other.go" }},
		{"file traversal", func(q *LineQuestion) { q.File = "../main.go" }},
		{"before", func(q *LineQuestion) { q.Line = 1 }},
		{"after", func(q *LineQuestion) { q.Line = 6 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := batchQuestions()[0]
			tc.change(&q)
			if _, err := r.WithLineQuestions([]LineQuestion{q}); err == nil {
				t.Fatal("accepted invalid question")
			}
		})
	}
	q := batchQuestions()[0]
	if _, err := r.WithLineQuestions([]LineQuestion{q, q}); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
	q.Text = strings.Repeat("x", maxPurpose)
	if _, err := r.WithLineQuestions([]LineQuestion{q}); err != nil {
		t.Fatal(err)
	}
	// Combined JSON-escaped question data, not just per-question lengths, is bounded.
	var many []LineQuestion
	for i := 0; i < 20; i++ {
		q.ID = fmt.Sprint(i)
		q.Text = strings.Repeat("<", 2000)
		many = append(many, q)
	}
	if _, err := r.WithLineQuestions(many); err == nil {
		t.Fatal("accepted oversized batch")
	}
}

func TestLineQuestionsPreflightBoundsRequiredUnion(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	var questions []LineQuestion
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("p.F%d", i)
		r.candidates = append(r.candidates, candidate{ID: id, File: "f.go", Line: 1, End: 1})
		r.snippets[id] = strings.Repeat("x", 6000)
		// Twelve selected sources fit, but their union with fourteen required do not.
		if i < 14 {
			questions = append(questions, LineQuestion{ID: id, Function: id, File: "f.go", Line: 1, Text: "why?"})
		}
	}
	if err := r.preflight(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WithLineQuestions(questions); err == nil {
		t.Fatal("accepted oversized required union")
	}
	r.lineQuestions = questions
	calls := 0
	_, err := r.Explain(context.Background(), func(context.Context, string) (string, error) { calls++; return "", nil })
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	// More than twelve required functions are allowed if the complete prompt fits.
	for id := range r.snippets {
		r.snippets[id] = "func F() {}"
	}
	batch, err := r.WithLineQuestions(questions)
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	_, err = batch.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
		calls++
		if calls == 1 {
			return `["` + batch.target + `"]`, nil
		}
		_, raw, _ := strings.Cut(prompt, "UNTRUSTED DATA:\n")
		var data struct {
			Sources []source `json:"sources"`
		}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Sources) != len(questions)+1 {
			t.Fatalf("dropped required sources: %d", len(data.Sources))
		}
		return "done", nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestParseLineAnswersStrict(t *testing.T) {
	questions := batchQuestions()
	valid := `{"answers":[{"id":"second","text":"two"},{"id":"first","text":"one"}]}`
	got, err := ParseLineAnswers(valid, questions)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"first": "one", "second": "two"}) {
		t.Fatalf("%v %v", got, err)
	}
	invalid := []string{"", "null", "[]", "```json\n" + valid + "\n```", valid + " trailing", valid + valid,
		`{"answers":null}`, `{"answers":{}}`, `{"answers":[]}`, `{"Answers":[]}`, `{"answers":[],"extra":true}`, `{"answers":[],"answers":[]}`,
		`{"answers":[null]}`, `{"answers":[{"id":"first","text":"one"}]}`,
		`{"answers":[{"id":"first","text":"one"},{"id":"first","text":"two"}]}`,
	}
	for _, tc := range []struct{ old, new string }{
		{`"second"`, `"unknown"`}, {`"second"`, `null`}, {`"second"`, `42`},
		{`"text":"two"`, `"text":null`}, {`"text":"two"`, `"text":42`}, {`"text":"two"`, `"text":"  "`},
		{`"text":"two"`, `"Text":"two"`}, {`"text":"two"`, `"text":"two","extra":true`},
		{`"text":"two"`, `"text":"two","text":"again"`}, {`"id":"second",`, ``}, {`,"text":"two"`, ``},
	} {
		invalid = append(invalid, strings.Replace(valid, tc.old, tc.new, 1))
	}
	invalid = append(invalid, strings.Repeat("x", maxPrompt+1), "\xff")
	for i, text := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got, err := ParseLineAnswers(text, questions); err == nil || got != nil {
				t.Fatalf("accepted %s", text)
			}
		})
	}
	if _, err := ParseLineAnswers(valid, nil); err == nil {
		t.Fatal("accepted no questions")
	}
	if _, err := ParseLineAnswers(valid, []LineQuestion{questions[0], questions[0]}); err == nil {
		t.Fatal("accepted duplicate question IDs")
	}
}
