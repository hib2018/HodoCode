// Package repository acquires immutable GitHub source snapshots without running
// git or repository code. Fetch resolves refs online on every call, even on cache
// hits. Cache directories must be user-owned and not concurrently modified by
// untrusted processes. Callers must treat snapshots as read-only.
package repository

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Options controls acquisition. Empty Ref means HEAD; zero Timeout and MaxBytes
// mean two minutes and 100 MiB respectively. MaxBytes independently bounds the
// compressed archive and its entire expanded tar stream (including headers).
// Empty CacheDir uses the OS user cache directory. Private alone permits invoking
// gh auth token; credentials are sent only to api.github.com, never on redirects.
// Refresh downloads again, but never replaces an already published snapshot.
type Options struct {
	URL, Ref, CacheDir string
	Timeout            time.Duration
	MaxBytes           int64
	Refresh, Private   bool
}

// Snapshot identifies a read-only directory at a resolved commit.
type Snapshot struct{ Root, URL, SHA string }

type dependencies struct {
	transport http.RoundTripper
	token     func(context.Context) (string, error)
}

// Fetch resolves a GitHub ref and atomically caches its safely extracted tarball.
func Fetch(ctx context.Context, opts Options) (*Snapshot, error) {
	return fetch(ctx, opts, dependencies{http.DefaultTransport, githubToken})
}

func githubToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", errors.New("GitHub authentication failed")
	}
	token := strings.TrimSpace(string(out))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("invalid GitHub authentication token")
	}
	return token, nil
}

var repoPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var commitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func repositoryURL(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" {
		return "", "", errors.New("repository URL must be https://github.com/owner/repo")
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "" {
		return "", "", errors.New("invalid repository path")
	}
	parts[2] = strings.TrimSuffix(parts[2], ".git")
	for _, s := range parts[1:] {
		if !repoPart.MatchString(s) || s == "." || s == ".." {
			return "", "", errors.New("invalid repository name")
		}
	}
	slug := strings.ToLower(parts[1] + "/" + parts[2])
	return "https://github.com/" + slug, slug, nil
}

func cachePath(cacheDir, canonical string) (string, error) {
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", errors.New("user cache directory unavailable")
		}
		cacheDir = filepath.Join(base, "hodocode", "repositories")
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(cacheDir, hex.EncodeToString(sum[:])), nil
}

func allowedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "github.com", "api.github.com", "codeload.github.com":
		return true
	}
	return false
}

func fetch(ctx context.Context, opts Options, deps dependencies) (*Snapshot, error) {
	canonical, slug, err := repositoryURL(opts.URL)
	if err != nil {
		return nil, err
	}
	if opts.Timeout < 0 || opts.MaxBytes < 0 {
		return nil, errors.New("negative acquisition limit")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 100 * 1024 * 1024
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	token := ""
	if opts.Private {
		token, err = deps.token(ctx)
		if err != nil {
			return nil, errors.New("GitHub authentication failed")
		}
	}
	client := &http.Client{Transport: deps.transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Authorization")
		if len(via) >= 10 || !allowedURL(req.URL) {
			return errors.New("GitHub redirect refused")
		}
		return nil
	}}
	get := func(endpoint string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, errors.New("invalid GitHub request")
		}
		if !allowedURL(req.URL) {
			return nil, errors.New("GitHub host refused")
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "HodoCode")
		req.Header.Set("Accept-Encoding", "identity")
		if token != "" && req.URL.Host == "api.github.com" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("GitHub request failed")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub request returned HTTP %d", resp.StatusCode)
		}
		return resp, nil
	}
	ref := opts.Ref
	if ref == "" {
		ref = "HEAD"
	}
	resp, err := get("https://api.github.com/repos/" + slug + "/commits/" + url.PathEscape(ref))
	if err != nil {
		return nil, err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	data, err := io.ReadAll(&contextReader{ctx, &boundedReader{r: resp.Body, remaining: 1024 * 1024}})
	resp.Body.Close()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || json.Unmarshal(data, &commit) != nil || !commitSHA.MatchString(commit.SHA) {
		return nil, errors.New("invalid GitHub commit response")
	}
	commit.SHA = strings.ToLower(commit.SHA)
	repoCache, err := cachePath(opts.CacheDir, canonical)
	if err != nil {
		return nil, err
	}
	final := filepath.Join(repoCache, commit.SHA)
	snapshot := &Snapshot{Root: filepath.Join(final, "tree"), URL: canonical, SHA: commit.SHA}
	present, err := cached(final)
	if err != nil {
		return nil, err
	}
	if present && !opts.Refresh {
		return snapshot, nil
	}
	if err = os.MkdirAll(repoCache, 0700); err != nil {
		return nil, errors.New("cannot create repository cache")
	}
	temp, err := os.MkdirTemp(repoCache, ".fetch-")
	if err != nil {
		return nil, errors.New("cannot create snapshot staging directory")
	}
	defer os.RemoveAll(temp)
	tree := filepath.Join(temp, "tree")
	if err = os.Mkdir(tree, 0700); err != nil {
		return nil, err
	}
	resp, err = get("https://api.github.com/repos/" + slug + "/tarball/" + commit.SHA)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Disable transparent gzip decoding so the compressed-byte limit is real even
	// when a server supplies Content-Encoding. The archive itself is always gzip.
	if resp.Uncompressed {
		return nil, errors.New("unexpected HTTP archive decompression")
	}
	compressed := &contextReader{ctx, &boundedReader{r: resp.Body, remaining: opts.MaxBytes}}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, errors.New("invalid GitHub archive")
	}
	defer gz.Close()
	expanded := &contextReader{ctx, &boundedReader{r: gz, remaining: opts.MaxBytes}}
	if err = extract(expanded, tree, opts.MaxBytes); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	// Read to EOF to enforce limits and verify gzip checksums and trailing members.
	if _, err = io.Copy(io.Discard, expanded); err != nil {
		return nil, errors.New("invalid or oversized expanded archive")
	}
	if _, err = io.Copy(io.Discard, compressed); err != nil {
		return nil, errors.New("oversized compressed archive")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(temp, "complete"), []byte(commit.SHA), 0600); err != nil {
		return nil, err
	}
	if err = os.Rename(temp, final); err != nil {
		if ok, checkErr := cached(final); checkErr != nil || !ok {
			return nil, errors.New("cannot publish repository snapshot")
		}
	}
	return snapshot, nil
}

func cached(dir string) (bool, error) {
	st, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || !st.IsDir() {
		return false, errors.New("invalid snapshot cache")
	}
	for _, name := range []string{"tree", "complete"} {
		st, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || st.Mode()&os.ModeSymlink != 0 || (name == "tree" && !st.IsDir()) || (name == "complete" && !st.Mode().IsRegular()) {
			return false, errors.New("incomplete snapshot cache")
		}
	}
	return true, nil
}

// RemoveCache removes only the named repository's cache, not the cache base.
// Callers must not remove a cache while another caller is using its snapshots.
func RemoveCache(cacheDir, rawURL string) error {
	canonical, _, err := repositoryURL(rawURL)
	if err != nil {
		return err
	}
	dir, err := cachePath(cacheDir, canonical)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type boundedReader struct {
	r         io.Reader
	remaining int64
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n != 0 {
			return 0, errors.New("archive byte limit exceeded")
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func extract(r io.Reader, root string, remaining int64) error {
	tr := tar.NewReader(r)
	top := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if top == "" {
				return errors.New("empty archive")
			}
			return nil
		}
		if err != nil {
			return errors.New("invalid tar archive")
		}
		// GitHub archives include commit metadata, not a filesystem entry.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\:\x00") || path.IsAbs(name) || path.Clean(name) != name {
			return errors.New("unsafe archive path")
		}
		parts := strings.Split(name, "/")
		if parts[0] == "." || parts[0] == ".." || strings.Contains(parts[0], ":") {
			return errors.New("unsafe archive root")
		}
		if top == "" {
			top = parts[0]
		}
		if parts[0] != top {
			return errors.New("multiple archive roots")
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return errors.New("archive links and special files refused")
		}
		if len(parts) == 1 {
			if h.Typeflag != tar.TypeDir {
				return errors.New("invalid archive root")
			}
			continue
		}
		// Sparse PAX archives can synthesize bytes not present in the tar stream.
		if h.Size < 0 || h.Size > remaining {
			return errors.New("expanded file byte limit exceeded")
		}
		remaining -= h.Size
		target := filepath.Join(root, filepath.FromSlash(strings.Join(parts[1:], "/")))
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0700); err != nil {
				return errors.New("cannot create archive directory")
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return errors.New("cannot create archive parent")
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("cannot create archive file")
		}
		_, copyErr := io.Copy(f, tr)
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("cannot copy archive file")
		}
	}
}
