package explain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// LineQuestion asks about an absolute, 1-based line of a prepared local function.
// ID is an opaque correlation key; ID and Text are limited to 8 KiB each.
type LineQuestion struct {
	ID       string `json:"id"`
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
}

func questionIDs(questions []LineQuestion) (map[string]bool, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("at least one line question required")
	}
	ids := make(map[string]bool, len(questions))
	for _, q := range questions {
		if strings.TrimSpace(q.ID) == "" || len(q.ID) > maxPurpose || !utf8.ValidString(q.ID) || ids[q.ID] {
			return nil, fmt.Errorf("question IDs must be nonempty, unique valid UTF-8 and at most %d bytes", maxPurpose)
		}
		if strings.TrimSpace(q.Text) == "" || len(q.Text) > maxPurpose || !utf8.ValidString(q.Text) {
			return nil, fmt.Errorf("question text must be nonempty valid UTF-8 and at most %d bytes", maxPurpose)
		}
		ids[q.ID] = true
	}
	return ids, nil
}

// WithLineQuestions returns an independent batch request. It replaces conversation
// and line-comment modes. Required snippets are included regardless of selection.
// Question text, like purpose, is user-provided and is not source-redacted.
func (r *Request) WithLineQuestions(questions []LineQuestion) (*Request, error) {
	if r == nil || r.target == "" || len(r.candidates) == 0 {
		return nil, fmt.Errorf("prepared request required")
	}
	if _, ok := r.snippets[r.target]; !ok {
		return nil, fmt.Errorf("prepared target required")
	}
	if _, err := questionIDs(questions); err != nil {
		return nil, err
	}
	for _, q := range questions {
		if _, ok := r.snippets[q.Function]; !ok {
			return nil, fmt.Errorf("question function is not prepared: %s", q.Function)
		}
		valid := false
		for _, c := range r.candidates {
			if c.ID == q.Function && c.File == q.File && q.Line >= c.Line && q.Line <= c.End {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("question file or line does not match prepared function: %s", q.ID)
		}
	}
	next := r.clone()
	next.lineQuestions = append([]LineQuestion(nil), questions...)
	next.lineComments = false
	next.history, next.question = nil, ""
	if err := next.preflight(); err != nil {
		return nil, err
	}
	return next, nil
}

func (r *Request) requiredQuestionFunctions() map[string]bool {
	required := make(map[string]bool, len(r.lineQuestions))
	for _, q := range r.lineQuestions {
		required[q.Function] = true
	}
	return required
}

// ParseLineAnswers accepts only strict JSON with exactly one nonempty answer per
// question ID, rejecting unknown/missing/duplicate IDs, fields and trailing data.
func ParseLineAnswers(text string, questions []LineQuestion) (map[string]string, error) {
	ids, err := questionIDs(questions)
	if err != nil {
		return nil, err
	}
	if len(text) > maxPrompt || !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid or oversized line answers")
	}
	fields, err := lineObject([]byte(text), []string{"answers"})
	if err != nil {
		return nil, err
	}
	var answers []json.RawMessage
	if err := json.Unmarshal(fields["answers"], &answers); err != nil {
		return nil, fmt.Errorf("answers: %w", err)
	}
	result := make(map[string]string, len(answers))
	for _, raw := range answers {
		fields, err := lineObject(raw, []string{"id", "text"})
		if err != nil {
			return nil, err
		}
		var id, answer string
		if err := json.Unmarshal(fields["id"], &id); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fields["text"], &answer); err != nil {
			return nil, err
		}
		if !ids[id] || strings.TrimSpace(answer) == "" {
			return nil, fmt.Errorf("unknown ID or empty answer")
		}
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("duplicate answer ID: %s", id)
		}
		result[id] = answer
	}
	if len(result) != len(ids) {
		return nil, fmt.Errorf("missing line answers")
	}
	return result, nil
}

const lineAnswerInstructions = `Return ONLY strict JSON with this schema: {"answers":[{"id":string,"text":string}]}. Return exactly one answer for every line_questions ID, using its exact ID. Answer only the corresponding question about its specified function and line. Answer text must be nonempty Japanese strings. Do not use Markdown fences, prose outside JSON, extra fields, duplicate keys, null values, unknown IDs or duplicate IDs. Distinguish facts from inference and cite file:line within answer text.
`
