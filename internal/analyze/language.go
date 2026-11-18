package analyze

import "context"

// LoadLanguage indexes the selected language, or all detected languages for auto.
// Go retains its module/build-aware loader; other languages require a language server.
// Servers are external processes: use only trusted projects and tools.
func LoadLanguage(root, language string) (*Project, error) {
	return LoadLanguageContext(context.Background(), root, language)
}

// LoadLanguageContext cancels language-server analysis with ctx.
func LoadLanguageContext(ctx context.Context, root, language string) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := loadPolyglot(root, "", language)
	if err != nil {
		return nil, err
	}
	if err := p.analyzeSemantics(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// FileLanguage identifies a supported language by its source filename extension.
// Unknown extensions return an empty string.
func FileLanguage(filename string) string {
	return polyExtension(filename)
}

// FunctionLanguage returns the language of an indexed function. Empty metadata
// remains Go for compatibility with existing callers constructing Projects.
func (p *Project) FunctionLanguage(id string) string {
	if language := p.Functions[id].Language; language != "" {
		return language
	}
	return "go"
}
