package analyze

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"strings"
	"text/scanner"
)

func moduleDirectory(dir string) string {
	if dir == "" {
		return ""
	}
	for {
		if stat, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && stat.Mode().IsRegular() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Match Go's automatic vendor selection: a go directive >=1.14, a vendor
// directory, and a manifest made in the same module/workspace mode. Explicit
// -mod is honored, except writable mod mode is downgraded to readonly.
func dependencyMode(origin, versionFile, base string, workspace bool, env []string) (string, error) {
	flags := os.Getenv("GOFLAGS")
	if flags == "" {
		// env only reads configuration. The other offline/toolchain guards still apply.
		data, err := offlineGo(origin, append(append([]string(nil), env...), "GOFLAGS="), "env", "GOFLAGS")
		if err != nil {
			return "", err
		}
		flags = string(data)
	}
	mode, err := moduleModeFlag(flags)
	if err != nil {
		return "", err
	}
	switch mode {
	case "vendor":
		return "vendor", nil
	case "mod", "readonly":
		return "readonly", nil
	case "":
	default:
		return "", fmt.Errorf("unsupported -mod setting %q", mode)
	}
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return "", err
	}
	// Only the first token of each directive line can be the go directive.
	var scan scanner.Scanner
	scan.Init(strings.NewReader(string(data)))
	scan.Whitespace ^= 1 << '\n'
	start := true
	goVersion := ""
	for tok := scan.Scan(); tok != scanner.EOF; tok = scan.Scan() {
		if tok == '\n' {
			start = true
			continue
		}
		if start && scan.TokenText() == "go" {
			scan.Scan()
			goVersion = "go" + scan.TokenText()
			break
		}
		start = false
	}
	if !version.IsValid(goVersion) || version.Compare(goVersion, "go1.14") < 0 {
		return "readonly", nil
	}
	vendor := filepath.Join(base, "vendor")
	stat, err := os.Stat(vendor)
	if os.IsNotExist(err) {
		return "readonly", nil
	}
	if err != nil {
		return "", err
	}
	if !stat.IsDir() {
		return "readonly", nil
	}
	manifest, err := os.ReadFile(filepath.Join(vendor, "modules.txt"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	line, _, _ := strings.Cut(string(manifest), "\n")
	annotations, _ := strings.CutPrefix(line, "## ")
	vendoredWorkspace := false
	for _, annotation := range strings.Split(annotations, ";") {
		if strings.TrimSpace(annotation) == "workspace" {
			vendoredWorkspace = true
		}
	}
	if vendoredWorkspace != workspace {
		return "readonly", nil
	}
	return "vendor", nil
}

// GOFLAGS uses Go's simple quoted-word syntax, not shell expansion. Only -mod
// survives here; all other flags remain excluded from metadata commands.
func moduleModeFlag(flags string) (string, error) {
	mode := ""
	for flags = strings.TrimSpace(flags); flags != ""; flags = strings.TrimSpace(flags) {
		var word string
		if flags[0] == '\'' || flags[0] == '"' {
			end := strings.IndexByte(flags[1:], flags[0])
			if end < 0 {
				return "", fmt.Errorf("unterminated quoted GOFLAGS")
			}
			word, flags = flags[1:end+1], flags[end+2:]
		} else {
			end := strings.IndexAny(flags, " \t\r\n")
			if end < 0 {
				end = len(flags)
			}
			word, flags = flags[:end], flags[end:]
		}
		if value, ok := strings.CutPrefix(word, "-mod="); ok {
			mode = value
		}
	}
	return mode, nil
}

func dependencyLabel(pkg string, module *dependencyModule, vendor, workspace bool) (string, error) {
	if module.Path == "" || (pkg != module.Path && !strings.HasPrefix(pkg, module.Path+"/")) {
		return "", fmt.Errorf("dependency module does not own %s", pkg)
	}
	selected := module.Version
	if selected == "" {
		selected = "local"
		if module.Main && workspace {
			selected = "workspace"
		}
	}
	label := module.Path + "@" + selected
	var origins []string
	if replacement := module.Replace; replacement != nil {
		target := replacement.Path
		if replacement.Version != "" {
			target += "@" + replacement.Version
		} else if replacement.Dir != "" {
			target = filepath.ToSlash(replacement.Dir)
		}
		origins = append(origins, "replace => "+target)
	}
	if workspace {
		origins = append(origins, "workspace")
	}
	if vendor {
		origins = append(origins, "vendor")
	}
	if len(origins) > 0 {
		label += " [" + strings.Join(origins, ", ") + "]"
	}
	return label + strings.TrimPrefix(pkg, module.Path), nil
}
