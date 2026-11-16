package explain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLineExplanation(t *testing.T) {
	text := `{"summary":"概要","comments":[{"file":"main.go","function":"p.F","line":2,"end_line":4,"text":"説明"},{"file":"other.go","function":"p.G","line":-1,"text":"位置はUIで検証"}]}`
	got, err := ParseLineExplanation(text)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "概要" || len(got.Comments) != 2 || got.Comments[0].EndLine != 4 || got.Comments[1].Line != -1 || got.Comments[1].EndLine != 0 {
		t.Fatalf("got %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"end_line"`) != 1 {
		t.Fatalf("optional end_line: %s", data)
	}
	if _, err := ParseLineExplanation(`{"summary":"概要","comments":[]}`); err != nil {
		t.Fatal(err)
	}
}

func TestParseLineExplanationRejectsInvalidSchema(t *testing.T) {
	valid := `{"summary":"summary","comments":[{"file":"main.go","function":"p.F","line":2,"text":"text"}]}`
	invalid := []string{"", "null", "[]", "```json\n" + valid + "\n```", valid + " trailing", valid + valid,
		`{"summary":"summary"}`, `{"summary":"summary","comments":null}`, `{"summary":"summary","comments":{}}`,
		`{"summary":null,"comments":[]}`, `{"summary":3,"comments":[]}`, `{"summary":"  \n","comments":[]}`, `{"comments":[]}`,
		`{"summary":"summary","comments":[],"extra":true}`, `{"Summary":"summary","comments":[]}`, `{"summary":"a","summary":"b","comments":[]}`,
	}
	for _, field := range []string{`"file":"main.go",`, `"function":"p.F",`, `"line":2,`, `,"text":"text"`} {
		invalid = append(invalid, strings.Replace(valid, field, "", 1))
	}
	for _, replacement := range []struct{ old, new string }{
		{`"main.go"`, `null`}, {`"p.F"`, `null`}, {`"p.F"`, `42`}, {`"line":2`, `"line":null`}, {`"line":2`, `"line":2.5`}, {`"line":2`, `"line":"2"`}, {`"text":"text"`, `"text":null`}, {`"text":"text"`, `"text":"  "`},
		{`"text":"text"`, `"text":"text","end_line":null`}, {`"text":"text"`, `"text":"text","end_line":1.5`}, {`"text":"text"`, `"text":"text","extra":true`}, {`"text":"text"`, `"text":"text","text":"again"`},
	} {
		invalid = append(invalid, strings.Replace(valid, replacement.old, replacement.new, 1))
	}
	invalid = append(invalid, `{"summary":"summary","comments":[null]}`)
	for _, text := range invalid {
		t.Run(text, func(t *testing.T) {
			if _, err := ParseLineExplanation(text); err == nil {
				t.Fatalf("accepted %s", text)
			}
		})
	}
}

func TestLineCommentsOptInAndConversation(t *testing.T) {
	base := prepare(t, fixture(t), nil)
	structured, err := base.WithLineComments()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := structured.WithConversation(nil, "why?")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := plain.WithLineComments()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		request    *Request
		structured bool
	}{{"base", base, false}, {"structured", structured, true}, {"followup", plain, false}, {"explicit followup", explicit, true}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			want := "plain answer"
			if tc.structured {
				want = `{"summary":"summary","comments":[]}`
			}
			answer, err := tc.request.Explain(context.Background(), func(_ context.Context, prompt string) (string, error) {
				calls++
				if calls == 1 {
					return `["` + base.target + `"]`, nil
				}
				if strings.Contains(prompt, `{"summary":`) != tc.structured {
					t.Errorf("wrong mode: %s", prompt)
				}
				if tc.structured {
					for _, instruction := range []string{"strict JSON", "end_line", "exact source", "日本語"} {
						if !strings.Contains(prompt, instruction) {
							t.Errorf("missing %q", instruction)
						}
					}
				}
				return want, nil
			})
			if err != nil || answer != want || calls != 2 {
				t.Fatalf("answer=%q err=%v calls=%d", answer, err, calls)
			}
		})
	}
	// A derived request must not share mutable snapshot storage.
	base.candidates[0].File = "changed.go"
	base.snippets[base.target] = "changed"
	if structured.candidates[0].File == "changed.go" || structured.snippets[base.target] == "changed" {
		t.Fatal("snapshot aliases base")
	}
	for _, invalid := range []*Request{nil, {}, {target: "missing", candidates: []candidate{{ID: "missing"}}}} {
		if _, err := invalid.WithLineComments(); err == nil {
			t.Fatal("accepted unprepared request")
		}
	}
}

func TestLineCommentsPreflightIncludesInstructions(t *testing.T) {
	r := prepare(t, fixture(t), nil)
	sources := make([]source, 0, len(r.candidates))
	for _, c := range r.candidates {
		sources = append(sources, source{c, r.snippets[c.ID]})
	}
	prompt, err := r.answerPrompt(sources, r.graph)
	if err != nil {
		t.Fatal(err)
	}
	// Fill the plain prompt exactly; structured instructions must be checked too.
	r.snippets[r.target] += strings.Repeat("x", maxPrompt-len(prompt))
	if err := r.preflight(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WithLineComments(); err == nil || !strings.Contains(err.Error(), "prompt exceeds") {
		t.Fatalf("structured overflow: %v", err)
	}
}
