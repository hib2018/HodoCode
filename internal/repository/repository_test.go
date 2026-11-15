package repository

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const sha = "0123456789abcdef0123456789abcdef01234567"
const repo = "https://github.com/Owner/Repo.git"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func archive(t *testing.T, headers ...tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		if h.Mode == 0 && h.Typeflag != tar.TypeXGlobalHeader {
			h.Mode = 0644
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := io.CopyN(tw, strings.NewReader(strings.Repeat("x", int(h.Size))), h.Size); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func goodArchive(t *testing.T) []byte {
	return archive(t, tar.Header{Name: "root/", Typeflag: tar.TypeDir}, tar.Header{Name: "root/main.go", Size: 3, Typeflag: tar.TypeReg})
}

func fixture(t *testing.T, handler http.HandlerFunc) dependencies {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	return dependencies{transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// Validate the real URL first; rewrite only inside the test transport.
		if !allowedURL(r.URL) {
			t.Errorf("untrusted URL reached transport: %s", r.URL)
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		clone.URL.Scheme, clone.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(clone)
	}), token: func(context.Context) (string, error) {
		t.Error("public fetch requested token")
		return "", errors.New("unexpected")
	}}
}
func apiHandler(data []byte, calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			*calls++
			io.WriteString(w, `{"sha":"`+sha+`"}`)
			return
		}
		w.Write(data)
	}
}
func opts(t *testing.T) Options { return Options{URL: repo, CacheDir: t.TempDir()} }

func TestFetchCacheRefreshAndRemove(t *testing.T) {
	data := goodArchive(t)
	resolves, downloads := 0, 0
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			resolves++
			if r.URL.EscapedPath() != "/repos/owner/repo/commits/feature%2Fsafe%3Fref" {
				t.Errorf("unsafe ref encoding: %s", r.URL.EscapedPath())
			}
			io.WriteString(w, `{"sha":"`+sha+`"}`)
			return
		}
		downloads++
		w.Write(data)
	})
	o := opts(t)
	o.Ref = "feature/safe?ref"
	first, err := fetch(context.Background(), o, deps)
	if err != nil {
		t.Fatal(err)
	}
	if first.URL != "https://github.com/owner/repo" || first.SHA != sha {
		t.Fatalf("snapshot: %+v", first)
	}
	content, err := os.ReadFile(filepath.Join(first.Root, "main.go"))
	if err != nil || string(content) != "xxx" {
		t.Fatalf("content %q: %v", content, err)
	}
	second, err := fetch(context.Background(), o, deps)
	if err != nil || second.Root != first.Root {
		t.Fatalf("cache: %v", err)
	}
	if resolves != 2 || downloads != 1 {
		t.Fatalf("requests: %d/%d", resolves, downloads)
	}
	o.Refresh = true
	data = []byte("broken archive")
	if _, err = fetch(context.Background(), o, deps); err == nil {
		t.Fatal("bad refresh succeeded")
	}
	if _, err = os.Stat(filepath.Join(first.Root, "main.go")); err != nil {
		t.Fatal("refresh damaged old snapshot", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(filepath.Dir(first.Root)))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fetch-") {
			t.Fatal("staging leaked")
		}
	}
	if err = RemoveCache(o.CacheDir, repo); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(first.Root); !os.IsNotExist(err) {
		t.Fatal("cache was not removed")
	}
}

func TestStrictURLs(t *testing.T) {
	for _, raw := range []string{"http://github.com/o/r", "https://github.com.evil/o/r", "https://github.com:443/o/r", "https://user:secret@github.com/o/r", "https://github.com/o/r?x", "https://github.com/o/r#x", "https://github.com/o/r/extra", "https://github.com/o/%72", "https://github.com/../r", "https://github.com/o/.git", "https://github.com/o/r//", "https://github.com/o/r?"} {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := repositoryURL(raw); err == nil {
				t.Fatal("accepted unsafe URL")
			}
		})
	}
}

func TestArchiveProtections(t *testing.T) {
	tests := []struct {
		name    string
		headers []tar.Header
	}{
		{"traversal", []tar.Header{{Name: "root/../../escape", Typeflag: tar.TypeReg}}},
		{"absolute", []tar.Header{{Name: "/outside", Typeflag: tar.TypeReg}}},
		{"backslash", []tar.Header{{Name: `root/..\escape`, Typeflag: tar.TypeReg}}},
		{"symlink", []tar.Header{{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}}},
		{"hardlink", []tar.Header{{Name: "root/link", Typeflag: tar.TypeLink, Linkname: "root/file"}}},
		{"fifo", []tar.Header{{Name: "root/pipe", Typeflag: tar.TypeFifo}}},
		{"device", []tar.Header{{Name: "root/dev", Typeflag: tar.TypeChar}}},
		{"roots", []tar.Header{{Name: "one/a", Typeflag: tar.TypeReg}, {Name: "two/b", Typeflag: tar.TypeReg}}},
		{"duplicate", []tar.Header{{Name: "root/a", Typeflag: tar.TypeReg}, {Name: "root/a", Typeflag: tar.TypeReg}}},
		{"empty", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			o := opts(t)
			data := archive(t, tt.headers...)
			if _, err := fetch(context.Background(), o, fixture(t, apiHandler(data, &calls))); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			matches, _ := filepath.Glob(filepath.Join(o.CacheDir, "*", "*"))
			if len(matches) != 0 {
				t.Fatalf("failed extraction left cache: %v", matches)
			}
		})
	}
}

func TestArchiveLimitsAndCorruption(t *testing.T) {
	good := goodArchive(t)
	corrupt := append([]byte(nil), good...)
	corrupt[len(corrupt)-5] ^= 0xff
	cases := []struct {
		name  string
		data  []byte
		limit int64
	}{
		{"compressed", good, 10},
		{"expanded", archive(t, tar.Header{Name: "root/big", Typeflag: tar.TypeReg, Size: 8192}), 4096},
		{"checksum", corrupt, 1 << 20},
		{"trailing compressed", append(append([]byte(nil), good...), bytes.Repeat([]byte("x"), 4096)...), 2048},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := opts(t)
			o.MaxBytes = tt.limit
			calls := 0
			if _, err := fetch(context.Background(), o, fixture(t, apiHandler(tt.data, &calls))); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
}

func TestPrivateRedirectCredentialsAndHostPolicy(t *testing.T) {
	data := goodArchive(t)
	var seen []string
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if strings.Contains(r.URL.Path, "/commits/") {
			io.WriteString(w, `{"sha":"`+sha+`"}`)
			return
		}
		if strings.Contains(r.URL.Path, "/tarball/") {
			http.Redirect(w, r, "https://codeload.github.com/archive", http.StatusFound)
			return
		}
		w.Write(data)
	})
	tokenCalls := 0
	deps.token = func(context.Context) (string, error) { tokenCalls++; return "SECRET", nil }
	o := opts(t)
	o.Private = true
	if _, err := fetch(context.Background(), o, deps); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || len(seen) != 3 || seen[0] != "Bearer SECRET" || seen[1] != "Bearer SECRET" || seen[2] != "" {
		t.Fatalf("credential routing: %v", seen)
	}
	for _, host := range []string{"http://api.github.com/a", "https://evil.example/a", "https://api.github.com.evil/a", "https://codeload.github.com:443/a", "https://user@github.com/a"} {
		t.Run(host, func(t *testing.T) {
			d := fixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, host, http.StatusFound) })
			if _, err := fetch(context.Background(), opts(t), d); err == nil {
				t.Fatal("unsafe redirect followed")
			}
		})
	}
}

func TestErrorsDoNotExposeSecrets(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		d := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, "SECRET ERROR BODY")
		})
		_, err := fetch(context.Background(), opts(t), d)
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	d := dependencies{transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET TRANSPORT") })}
	_, err := fetch(context.Background(), opts(t), d)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
	o := opts(t)
	o.Private = true
	d.token = func(context.Context) (string, error) { return "", errors.New("SECRET TOKEN") }
	_, err = fetch(context.Background(), o, d)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	d := dependencies{transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	o := opts(t)
	o.Timeout = time.Millisecond
	if _, err := fetch(context.Background(), o, d); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetch(ctx, opts(t), d); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	reader := &contextReader{ctx: ctx, r: strings.NewReader("payload")}
	if _, err := io.ReadAll(reader); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentPublication(t *testing.T) {
	data := goodArchive(t)
	d := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			io.WriteString(w, `{"sha":"`+sha+`"}`)
			return
		}
		w.Write(data)
	})
	o := opts(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fetch(context.Background(), o, d); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
