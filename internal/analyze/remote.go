package analyze

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoadSubdir indexes only subdir, inheriting a module from its repository ancestors.
// Subdir must be relative and cannot contain traversal or symlink components.
func LoadSubdir(repositoryRoot, subdir string) (*Project, error) {
	return LoadSubdirLanguage(repositoryRoot, subdir, "auto")
}

// LoadSubdirLanguage selects a language within a validated repository subdirectory.
func LoadSubdirLanguage(repositoryRoot, subdir, language string) (*Project, error) {
	return LoadSubdirLanguageContext(context.Background(), repositoryRoot, subdir, language)
}

// LoadSubdirLanguageContext cancels language-server analysis with ctx.
func LoadSubdirLanguageContext(ctx context.Context, repositoryRoot, subdir, language string) (*Project, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("repository root: %w", err)
	}
	if !filepath.IsLocal(subdir) && subdir != "" {
		return nil, fmt.Errorf("subdirectory must stay within repository: %q", subdir)
	}
	selected := root
	for _, part := range strings.Split(filepath.ToSlash(subdir), "/") {
		if part == ".." {
			return nil, fmt.Errorf("subdirectory traversal: %q", subdir)
		}
		if part == "" || part == "." {
			continue
		}
		selected = filepath.Join(selected, part)
		info, err := os.Lstat(selected)
		if err != nil {
			return nil, fmt.Errorf("subdirectory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("subdirectory is not a real directory: %s", selected)
		}
	}
	p, err := loadPolyglot(selected, root, language)
	if err != nil {
		return nil, err
	}
	if err := p.analyzeSemantics(ctx); err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, selected)
	if err != nil {
		return nil, err
	}
	if relative != "." {
		p.RepositorySubdir = filepath.ToSlash(relative)
	}
	return p, nil
}

// SourceURL returns the immutable GitHub source location of a local function.
// Missing metadata, unknown functions and SDK functions have no remote location.
func (p *Project) SourceURL(id string) string {
	fn, ok := p.Functions[id]
	if !ok || !p.LocalFunction(id) || p.RepositoryURL == "" || p.CommitSHA == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(p.RepositorySubdir+"/"+fn.File, "/"), "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ""
		}
		parts[i] = url.PathEscape(part)
	}
	return strings.TrimRight(p.RepositoryURL, "/") + "/blob/" + url.PathEscape(p.CommitSHA) + "/" + strings.Join(parts, "/") + "#L" + strconv.Itoa(fn.Line) + "-L" + strconv.Itoa(fn.End)
}
