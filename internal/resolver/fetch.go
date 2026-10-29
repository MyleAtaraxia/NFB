package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	downloadAttempts = 3
	tempMaxAge       = 24 * time.Hour
	cacheMaxAge      = 30 * 24 * time.Hour
)

var (
	stallTimeout = time.Minute
	retryDelay   = 2 * time.Second
)

func hashKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Command stops the whole process when ctx ends. On Windows, killing docker or git alone leaves
// their helpers running, and those keep the build going and our pipes open.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 5 * time.Second
	if runtime.GOOS == "windows" {
		cmd.Cancel = func() error {
			return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		}
	}
	return cmd
}

// touch marks a cache entry as used, so sweep keeps it. Chtimes briefly blocks readers on
// Windows, and sweep only works in days, so entries touched recently are left alone.
func touch(path string) {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < tempMaxAge {
		return
	}
	now := time.Now()
	os.Chtimes(path, now, now)
}

// publish moves a finished temp file into the cache. Entries are named by their content,
// so if another run got there first, its result is just as good.
func publish(tmp, final string) error {
	if exists(final) {
		return nil
	}
	if err := os.Rename(tmp, final); err != nil && !exists(final) {
		return err
	}
	return nil
}

// sweep removes what killed runs left behind and entries nothing has used for a while.
func sweep(cache string) {
	for _, dir := range []string{cache, filepath.Join(cache, "git"), filepath.Join(cache, "downloads")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			age := time.Since(info.ModTime())
			temp := strings.HasPrefix(entry.Name(), "tmp-") || strings.HasPrefix(entry.Name(), "build-")
			if (temp && age > tempMaxAge) || (!temp && dir != cache && age > cacheMaxAge) {
				os.RemoveAll(filepath.Join(dir, entry.Name()))
			}
		}
	}
}

func (r *resolver) checkout(url, ref string, remote bool) (dir, commit string, err error) {
	key := url + "#" + ref
	commit = r.graph.oldLock.Git[key]
	if commit != "" && !r.graph.noCache {
		if dir = filepath.Join(r.cache, "git", hashKey(url, commit)); exists(dir) {
			touch(dir)
			r.graph.Lock.Git[key] = commit
			return dir, commit, nil
		}
	}

	if err := os.MkdirAll(filepath.Join(r.cache, "git"), 0o755); err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(filepath.Join(r.cache, "git"), "tmp-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)

	target := commit
	if target == "" {
		target = ref
	}
	fmt.Fprintln(os.Stderr, "fetching", url, "at", target)
	protocols := "http:https:ssh:git"
	if !remote {
		protocols += ":file"
	}
	git := func(args ...string) (string, error) {
		cmd := Command(r.ctx, "git", append([]string{"-C", tmp, "-c", "core.autocrlf=false"}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL="+protocols,
			"GIT_HTTP_LOW_SPEED_LIMIT=1000", fmt.Sprintf("GIT_HTTP_LOW_SPEED_TIME=%d", int(stallTimeout.Seconds())))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out)), nil
	}

	if _, err := git("init", "-q"); err != nil {
		return "", "", err
	}
	head := "FETCH_HEAD"
	if _, err := git("fetch", "-q", "--depth", "1", "--", url, target); err != nil {
		// Servers can refuse a commit that isn't a branch tip, so search the whole history.
		if _, fullErr := git("fetch", "-q", "--", url, "+refs/*:refs/nfb/*"); fullErr != nil {
			return "", "", fmt.Errorf("fetching %s at %s: %w", url, target, err)
		}
		head = target
	}
	if _, err := git("checkout", "-q", "--detach", head); err != nil {
		return "", "", fmt.Errorf("fetching %s at %s: %w", url, target, err)
	}
	// Some servers refuse shallow fetches of a pinned commit that isn't a branch tip.
	if _, err := git("submodule", "update", "--init", "--recursive", "--depth", "1"); err != nil {
		if _, err := git("submodule", "update", "--init", "--recursive"); err != nil {
			return "", "", fmt.Errorf("fetching submodules of %s: %w", url, err)
		}
	}
	if commit, err = git("rev-parse", "HEAD"); err != nil {
		return "", "", err
	}
	if err := removeGitData(tmp); err != nil {
		return "", "", err
	}

	dir = filepath.Join(r.cache, "git", hashKey(url, commit))
	if err := publish(tmp, dir); err != nil {
		return "", "", err
	}
	touch(dir)
	r.graph.Lock.Git[key] = commit
	return dir, commit, nil
}

var httpClient = http.DefaultClient

func fetch(ctx context.Context, raw string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	if token := hostToken(req.URL); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return httpClient.Do(req)
}

// hostToken is tied to the host, so a manifest can't send a token to a server of its choosing.
func hostToken(u *neturl.URL) string {
	if u.Scheme != "https" {
		return ""
	}
	return os.Getenv("NFB_TOKEN_" + strings.NewReplacer(".", "_", "-", "_").Replace(strings.ToUpper(u.Hostname())))
}

// removeGitData also covers the .git files submodules leave behind, which point at a deleted folder.
func removeGitData(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Name() != ".git" {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

// download keeps files by checksum, so a pinned file only touches the network once.
func (r *resolver) download(url, want string) (file, sum string, err error) {
	want = strings.ToLower(want)
	if want == "" {
		want = r.graph.oldLock.Checksums[url]
	}
	dir := filepath.Join(r.cache, "downloads")
	if file = filepath.Join(dir, want); want != "" && exists(file) && !r.graph.noCache {
		touch(file)
		r.graph.Lock.Checksums[url] = want
		return file, want, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	tmp, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	fmt.Fprintln(os.Stderr, "fetching", url)
	if sum, err = r.fetchTo(url, tmp); err != nil {
		return "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}

	if want != "" && sum != want {
		return "", "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, want, sum)
	}
	file = filepath.Join(dir, sum)
	if err := publish(tmp.Name(), file); err != nil {
		return "", "", err
	}
	touch(file)
	r.graph.Lock.Checksums[url] = sum
	return file, sum, nil
}

func (r *resolver) fetchTo(url string, dst *os.File) (string, error) {
	for attempt := 1; ; attempt++ {
		sum, retry, err := r.tryFetch(url, dst)
		if err == nil || !retry || attempt == downloadAttempts || r.ctx.Err() != nil {
			return sum, err
		}
		fmt.Fprintf(os.Stderr, "retrying after: %v\n", err)
		select {
		case <-r.ctx.Done():
			return "", r.ctx.Err()
		case <-time.After(retryDelay * time.Duration(attempt)):
		}
	}
}

func (r *resolver) tryFetch(url string, dst *os.File) (sum string, retry bool, err error) {
	if err := dst.Truncate(0); err != nil {
		return "", false, err
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return "", false, err
	}

	ctx, cancel := context.WithCancelCause(r.ctx)
	defer cancel(nil)
	stall := time.AfterFunc(stallTimeout, func() { cancel(fmt.Errorf("no data for %s", stallTimeout)) })
	defer stall.Stop()
	failed := func(err error) error {
		if cause := context.Cause(ctx); cause != nil {
			err = cause
		}
		return fmt.Errorf("downloading %s: %w", url, err)
	}

	resp, err := fetch(ctx, url)
	if err != nil {
		return "", true, failed(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		temporary := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout
		return "", temporary, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), &stallReader{resp.Body, stall}); err != nil {
		return "", true, failed(err)
	}
	return hex.EncodeToString(h.Sum(nil)), false, nil
}

// stallReader cancels slow downloads by silence rather than total time, so big files can still finish.
type stallReader struct {
	io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.Reader.Read(p)
	s.timer.Reset(stallTimeout)
	return n, err
}
