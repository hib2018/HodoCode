package analyze

// LoadLanguage indexes the selected language, or all detected languages for auto.
// Go retains its module/build-aware loader; other languages never execute tools.
func LoadLanguage(root, language string) (*Project, error) {
	return loadPolyglot(root, "", language)
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
