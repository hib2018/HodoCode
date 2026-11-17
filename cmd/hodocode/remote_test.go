package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type githubTransport func(*http.Request) (*http.Response, error)

func (f githubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockGitHub(t *testing.T) *int {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	files := []struct{ name, text string }{
		{"snapshot/go.mod", "module example.com/remote\n\ngo 1.23\n"},
		{"snapshot/main.go", "package remote\nfunc Hello() {}\n"},
		{"snapshot/sub/sub.go", "package sub\nfunc Child() {}\n"},
		{"snapshot/nested/go.mod", "module example.com/nested\n\ngo 1.23\n"},
		{"snapshot/nested/main.go", "package nested\nfunc Other() {}\n"},
		{"snapshot/python/app.py", "def entry():\n    return 'MASK_ME'\n"},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0600, Size: int64(len(f.text)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, f.text); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	downloads := new(int)
	http.DefaultTransport = githubTransport(func(r *http.Request) (*http.Response, error) {
		var body io.Reader
		switch {
		case strings.Contains(r.URL.Path, "/commits/"):
			body = strings.NewReader(`{"sha":"` + strings.Repeat("a", 40) + `"}`)
		case strings.Contains(r.URL.Path, "/tarball/"):
			*downloads++
			body = bytes.NewReader(archive.Bytes())
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(body), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	return downloads
}

func TestRemoteCommands(t *testing.T) {
	downloads := mockGitHub(t)
	cache := t.TempDir()
	for _, tc := range []struct {
		command string
		flags   []string
		want    string
	}{
		{"functions", nil, "https://github.com/owner/repo/blob/" + strings.Repeat("a", 40) + "/main.go#L2-L2"},
		{"deps", nil, "flowchart TD"},
		{"flow", []string{"--func", "Hello"}, "/main.go#L2-L2"},
		{"tui", []string{"--func", "Hello"}, "/main.go#L2-L2"},
		{"explain", []string{"--func", "Hello", "--purpose", "test"}, "/main.go#L2-L2"},
		{"functions", []string{"--subdir", "sub"}, "example.com/remote/sub.Child"},
		{"functions", []string{"--subdir", "python", "--language", "python"}, "app.py.entry"},
		{"flow", []string{"--subdir", "python", "--language", "python", "--func", "entry"}, "/python/app.py#L1-L2"},
		{"explain", []string{"--subdir", "python", "--language", "python", "--func", "entry", "--purpose", "test"}, "/python/app.py#L1-L2"},
	} {
		t.Run(tc.command+strings.Join(tc.flags, ""), func(t *testing.T) {
			args := append([]string{tc.command, "--repo", "https://github.com/owner/repo", "--cache-dir", cache}, tc.flags...)
			var out bytes.Buffer
			if err := run(context.Background(), args, strings.NewReader("q\n"), &out, io.Discard, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), strings.Repeat("a", 40)) {
				t.Fatal(out.String())
			}
			if tc.command == "functions" && len(tc.flags) == 0 && !strings.Contains(out.String(), "example.com/nested.Other") {
				t.Fatal("nested module omitted")
			}
			if len(tc.flags) > 0 && tc.flags[0] == "--subdir" && strings.Contains(out.String(), "remote.Hello") {
				t.Fatal("subdir indexed outside scope")
			}
		})
	}
	var preview bytes.Buffer
	if err := run(context.Background(), []string{"explain", "--repo", "https://github.com/owner/repo", "--cache-dir", cache, "--func", "Hello", "--purpose", "test", "--exclude", "nested"}, strings.NewReader(""), &preview, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(preview.String(), "example.com/nested.Other") {
		t.Fatal("remote preview ignored exclusion")
	}
	if *downloads != 1 {
		t.Fatalf("downloads=%d", *downloads)
	}
	if err := run(context.Background(), []string{"cache", "--repo", "https://github.com/owner/repo", "--cache-dir", cache, "--delete"}, strings.NewReader(""), io.Discard, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"functions", "--repo", "https://github.com/owner/repo", "--cache-dir", cache}, strings.NewReader(""), io.Discard, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if *downloads != 2 {
		t.Fatalf("cache was not deleted: downloads=%d", *downloads)
	}
}

func TestRemoteFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"functions", "--repo", "https://github.com/owner/repo", "--root", "."},
		{"functions", "--ref", "main"},
		{"functions", "--private"},
		{"functions", "--repo", "https://github.com/owner/repo", "--subdir", "../escape"},
		{"cache", "--repo", "https://github.com/owner/repo"},
	} {
		if err := run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard, nil); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
