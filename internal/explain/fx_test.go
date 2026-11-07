package explain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type invocation struct {
	Args    []string
	Dir     string
	Entries int
	Input   string
}

// The test binary itself is the fake executable; no installed fx is invoked.
func TestMain(m *testing.M) {
	if capture := os.Getenv("HODOCODE_FAKE_FX_CAPTURE"); capture != "" {
		dir, _ := os.Getwd()
		entries, _ := os.ReadDir(dir)
		input, _ := io.ReadAll(os.Stdin)
		data, _ := json.Marshal(invocation{os.Args[1:], dir, len(entries), string(input)})
		if err := os.WriteFile(capture, data, 0600); err != nil {
			os.Exit(90)
		}
		switch os.Getenv("HODOCODE_FAKE_FX_MODE") {
		case "fail":
			_, _ = os.Stdout.WriteString("partial output")
			_, _ = os.Stderr.WriteString("useful failure diagnostic")
			os.Exit(7)
		case "large":
			_, _ = os.Stdout.WriteString(strings.Repeat("x", maxPrompt+1))
		case "large-stderr":
			_, _ = os.Stderr.WriteString(strings.Repeat("x", maxPrompt+1))
		default:
			_, _ = os.Stdout.WriteString("fake explanation")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeFX(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(root, "fx")); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "capture.json")
	t.Setenv("PATH", root)
	t.Setenv("HODOCODE_FAKE_FX_CAPTURE", capture)
	t.Setenv("HODOCODE_FAKE_FX_MODE", "")
	return capture
}

func TestFXSubprocess(t *testing.T) {
	capture := fakeFX(t)
	const prompt = "line1\n$(touch should-not-exist)\n'quoted'"
	result, err := FX(context.Background(), prompt)
	if err != nil || result != "fake explanation" {
		t.Fatalf("%q, %v", result, err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var got invocation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, []string{"ask", "--no-save", "--no-color"}) || got.Input != prompt || got.Entries != 0 {
		t.Fatalf("%+v", got)
	}
	cwd, _ := os.Getwd()
	if got.Dir == cwd || got.Dir == filepath.Dir(capture) {
		t.Fatal("did not isolate working directory")
	}
	if _, err := os.Stat(got.Dir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory not removed: %v", err)
	}
	for _, mode := range []string{"fail", "large", "large-stderr"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HODOCODE_FAKE_FX_MODE", mode)
			_, err := FX(context.Background(), prompt)
			if err == nil {
				t.Fatal("expected failure")
			}
			if mode == "fail" && (!strings.Contains(err.Error(), "exit status 7") || !strings.Contains(err.Error(), "useful failure diagnostic") || !strings.Contains(err.Error(), "partial output")) {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(capture)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(got.Dir); !os.IsNotExist(err) {
				t.Fatal("failed command directory not cleaned up")
			}
		})
	}
}

func TestFXRejectsBeforeSubprocess(t *testing.T) {
	capture := fakeFX(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FX(ctx, "prompt"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := FX(context.Background(), strings.Repeat("x", maxPrompt+1)); err == nil {
		t.Fatal("accepted large prompt")
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("subprocess ran")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := FX(context.Background(), "prompt"); err == nil || !strings.Contains(err.Error(), "fx ask") {
		t.Fatal(err)
	}
}
