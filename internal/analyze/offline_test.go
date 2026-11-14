package analyze

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOfflineMetadataEnvironment(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(bin, "args")
	envFile := filepath.Join(bin, "env")
	config := filepath.Join(bin, "goenv")
	write(t, bin, "goenv", "GOPROXY=https://must-not-use.invalid\nGOTOOLCHAIN=go999.0.0\n")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	write(t, bin, "go", `#!/bin/sh
printf '%s\n' "$@" > "$TRACE_ARGS"
printf '%s\n' "$GOTOOLCHAIN" "$GOPROXY" "$GOSUMDB" "$GOFLAGS" "$GOENV" "$GONOPROXY" "$GOPRIVATE" "$GOVCS" "$GOWORK" > "$TRACE_ENV"
printf '{}'
`)
	if err := os.Chmod(filepath.Join(bin, "go"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("TRACE_ARGS", argsFile)
	t.Setenv("TRACE_ENV", envFile)
	t.Setenv("GOENV", config)
	t.Setenv("GOFLAGS", "-mod=mod -overlay=hostile.json")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "direct")
	t.Setenv("GONOPROXY", "*")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GOVCS", "*:all")
	args := []string{"list", "-e", "-json", "-mod=readonly", "example.com/dep"}
	if _, err := offlineGo(bin, []string{"GOWORK=off"}, args...); err != nil {
		t.Fatal(err)
	}
	gotArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(strings.Split(strings.TrimSuffix(string(gotArgs), "\n"), "\n"), args) {
		t.Fatalf("arguments: %s", gotArgs)
	}
	gotEnv, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotEnv) != "local\noff\noff\n \n"+config+"\nnone\n\n*:off\noff\n" {
		t.Fatalf("unsafe metadata env: %q", gotEnv)
	}
	after, err := os.ReadFile(config)
	if err != nil || string(after) != string(before) {
		t.Fatalf("modified config: %v", err)
	}
}
