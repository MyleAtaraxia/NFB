package resolver

import (
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
	"strings"
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

func (r *resolver) checkout(url, ref string, remote bool) (dir, commit string, err error) {
	key := url + "#" + ref
	commit = r.graph.oldLock.Git[key]
	if commit != "" && !r.graph.noCache {
		if dir = filepath.Join(r.cache, "git", hashKey(url, commit)); exists(dir) {
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
		cmd := exec.Command("git", append([]string{"-C", tmp, "-c", "core.autocrlf=false"}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL="+protocols)
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
	if r.graph.noCache {
		if err := os.RemoveAll(dir); err != nil {
			return "", "", err
		}
	}
	if !exists(dir) {
		if err := os.Rename(tmp, dir); err != nil {
			return "", "", err
		}
	}
	r.graph.Lock.Git[key] = commit
	return dir, commit, nil
}

var httpClient = http.DefaultClient

func fetch(raw string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
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
	resp, err := fetch(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("downloading %s: %s", url, resp.Status)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return "", "", fmt.Errorf("downloading %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}

	sum = hex.EncodeToString(h.Sum(nil))
	if want != "" && sum != want {
		return "", "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, want, sum)
	}
	file = filepath.Join(dir, sum)
	if err := os.Rename(tmp.Name(), file); err != nil {
		return "", "", err
	}
	r.graph.Lock.Checksums[url] = sum
	return file, sum, nil
}
