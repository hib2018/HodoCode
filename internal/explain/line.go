package explain

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// LineComment is an explanation anchored to a source function and line range.
// EndLine is optional (zero means omitted). Callers must validate all positions
// and source identities against the displayed source before rendering comments.
type LineComment struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	EndLine  int    `json:"end_line,omitempty"`
	Text     string `json:"text"`
}

// LineExplanation is the structured answer requested by WithLineComments.
type LineExplanation struct {
	Summary  string        `json:"summary"`
	Comments []LineComment `json:"comments"`
}

// ParseLineExplanation accepts only the strict JSON schema, without Markdown or
// surrounding prose. Source identities and positions are left for the UI to validate.
func ParseLineExplanation(text string) (LineExplanation, error) {
	var result LineExplanation
	fields, err := lineObject([]byte(text), []string{"summary", "comments"})
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(fields["summary"], &result.Summary); err != nil {
		return LineExplanation{}, fmt.Errorf("summary: %w", err)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return LineExplanation{}, fmt.Errorf("summary must be nonempty")
	}
	var comments []json.RawMessage
	if err := json.Unmarshal(fields["comments"], &comments); err != nil {
		return LineExplanation{}, fmt.Errorf("comments: %w", err)
	}
	result.Comments = make([]LineComment, 0, len(comments))
	for i, raw := range comments {
		if _, err := lineObject(raw, []string{"file", "function", "line", "text"}, "end_line"); err != nil {
			return LineExplanation{}, fmt.Errorf("comment %d: %w", i, err)
		}
		var comment LineComment
		if err := json.Unmarshal(raw, &comment); err != nil {
			return LineExplanation{}, fmt.Errorf("comment %d: %w", i, err)
		}
		if strings.TrimSpace(comment.Text) == "" {
			return LineExplanation{}, fmt.Errorf("comment %d text must be nonempty", i)
		}
		result.Comments = append(result.Comments, comment)
	}
	return result, nil
}

// Decode fields individually to reject duplicate keys, nulls and case variants
// that encoding/json otherwise accepts when decoding directly into a struct.
func lineObject(data []byte, required []string, optional ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, fmt.Errorf("unknown field %q", token)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if string(value) == "null" {
			return nil, fmt.Errorf("field %q cannot be null", key)
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected end of JSON object")
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("missing field %q", key)
		}
	}
	return fields, nil
}

// WithLineComments returns an independent snapshot requesting a strict JSON
// LineExplanation. Explain still returns the raw string; parse it separately.
// WithConversation resets this mode; opt in again for structured follow-ups.
func (r *Request) WithLineComments() (*Request, error) {
	if r == nil || r.target == "" || len(r.candidates) == 0 {
		return nil, fmt.Errorf("prepared request required")
	}
	if _, ok := r.snippets[r.target]; !ok {
		return nil, fmt.Errorf("prepared target required")
	}
	next := r.clone()
	next.lineComments = true
	if err := next.preflight(); err != nil {
		return nil, err
	}
	return next, nil
}

const lineInstructions = `Return ONLY strict JSON with this schema: {"summary":string,"comments":[{"file":string,"function":string,"line":int,"end_line":int(optional),"text":string}]}. Do not use Markdown fences, prose outside JSON, extra fields, duplicate keys or null values. Summary and comment text must be nonempty Japanese strings. Use exact source file paths and exact source function IDs; line and optional end_line are absolute, 1-based source line numbers within that function, with end_line >= line. Omit end_line for a single line. The summary must contain only the function role, broad processing flow and cautions; put line-level detail only in comments and do not repeat it in summary. Comments must explain only the target function, use its exact ID, and attach multi-line explanations to end_line; do not invent source positions. When revising comments, return the complete replacement comments array, not a patch. An empty comments array is allowed when no line comment is justified.
`
