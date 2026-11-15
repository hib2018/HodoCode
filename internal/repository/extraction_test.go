package repository

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGitHubPAXMetadata(t *testing.T) {
	metadata := tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": sha}}
	for _, tc := range []struct {
		name      string
		headers   []tar.Header
		wantError bool
	}{
		{"source", []tar.Header{{Name: "root/main.go", Typeflag: tar.TypeReg, Size: 3}}, false},
		{"metadata only", nil, true},
		{"symlink", []tar.Header{{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}}, true},
		{"hardlink", []tar.Header{{Name: "root/link", Typeflag: tar.TypeLink, Linkname: "root/main.go"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := archive(t, append([]tar.Header{metadata}, tc.headers...)...)
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			root := t.TempDir()
			err = extract(gz, root, 1<<20)
			if (err != nil) != tc.wantError {
				t.Fatalf("extract: %v", err)
			}
			if tc.wantError {
				return
			}
			if _, err := os.Stat(filepath.Join(root, "pax_global_header")); !os.IsNotExist(err) {
				t.Fatal("metadata written as a file")
			}
			if data, err := os.ReadFile(filepath.Join(root, "main.go")); err != nil || string(data) != "xxx" {
				t.Fatalf("source: %q, %v", data, err)
			}
		})
	}
}

func TestExtractFileOutputBudget(t *testing.T) {
	data := goodArchive(t)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	// Independent of the tar-stream limit, cap bytes synthesized by tar.Reader
	// (including sparse files) before opening or writing the destination file.
	if err := extract(gz, t.TempDir(), 2); err == nil {
		t.Fatal("file output exceeded budget")
	}
}
