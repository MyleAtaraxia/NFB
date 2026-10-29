package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pkg(name, deps string) string {
	return "[package]\nname = \"" + name + "\"\nversion = \"1\"\n[build]\nimage = \"x\"\n" + deps
}

func write(t *testing.T, file, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func tempCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LocalAppData", dir)
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
}

func local(path string) string {
	return "[[dependency]]\nsource = \"local\"\npath = \"" + path + "\"\n"
}

func TestBuildOrderAndDiamond(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	write(t, dir+"/c/package.nfb.toml", pkg("c", ""))
	write(t, dir+"/b/package.nfb.toml", pkg("b", local("../c/package.nfb.toml")))
	root := write(t, dir+"/package.nfb.toml", pkg("a", local("b/package.nfb.toml")+local("c/package.nfb.toml")))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range g.Units {
		names = append(names, u.Name())
	}
	if got := strings.Join(names, ","); got != "c,b,a" {
		t.Errorf("build order %s", got)
	}
}

func TestErrors(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	write(t, dir+"/v1/package.nfb.toml", pkg("dup", ""))
	write(t, dir+"/v2/package.nfb.toml", pkg("dup", ""))
	write(t, dir+"/project/build.nfb.toml", "[project]\nname = \"p\"\n[build]\nimage = \"x\"\ncommand = [\"y\"]\n[[output.artifact]]\nfrom = \"a\"\nto = \"b\"\n")
	write(t, dir+"/loop/a.toml", pkg("a", local("b.toml")))
	write(t, dir+"/loop/b.toml", pkg("b", local("a.toml")))

	cases := map[string]string{
		"two versions": write(t, dir+"/two.toml", pkg("r", local("v1/package.nfb.toml")+local("v2/package.nfb.toml"))),
		"project dep":  write(t, dir+"/proj.toml", pkg("r", local("project/build.nfb.toml"))),
		"cycle":        dir + "/loop/a.toml",
	}
	for name, root := range cases {
		if _, err := Resolve(context.Background(), root, Options{}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitPinsCommitAndRejectsLocalInside(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", pkg("remote", ""))
	write(t, repo+"/nested/evil.toml", pkg("evil", local("../secret.toml")))
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "one")
	first := git(t, repo, "rev-parse", "HEAD")

	dep := "[[dependency]]\nsource = \"git\"\nurl = \"" + filepath.ToSlash(repo) + "\"\nref = \"main\"\n"
	root := write(t, dir+"/package.nfb.toml", pkg("a", dep))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !g.LockChanged() {
		t.Error("a first resolve should change the lock")
	}
	if err := g.SaveLock(); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(g.Units[0].SourceDir, "package.nfb.toml")) {
		t.Error("source is not checked out")
	}

	write(t, repo+"/new.txt", "x")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "two")

	again, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Lock.Git[filepath.ToSlash(repo)+"#main"]; got != first {
		t.Errorf("lock moved to %s, want %s", got, first)
	}
	if again.LockChanged() {
		t.Error("a pinned resolve should not change the lock")
	}

	updated, err := Resolve(context.Background(), root, Options{Update: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Lock.Git[filepath.ToSlash(repo)+"#main"]; got == first {
		t.Error("update kept the old commit")
	}

	withPath := dep + "path = \"nested/evil.toml\"\n"
	if _, err := Resolve(context.Background(), write(t, dir+"/evil.toml", pkg("b", withPath)), Options{}); err == nil {
		t.Error("expected local inside a fetched package to fail")
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "commit")
}

func gitDep(repo string) string {
	return "[[dependency]]\nsource = \"git\"\nurl = \"" + filepath.ToSlash(repo) + "\"\nref = \"main\"\n"
}

func TestGitSubmodules(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()

	nested := filepath.Join(dir, "nested")
	write(t, nested+"/deep.txt", "deep\n")
	git(t, nested, "init", "-q", "-b", "main")
	commitAll(t, nested)

	sub := filepath.Join(dir, "sub")
	write(t, sub+"/lib.txt", "lib\n")
	git(t, sub, "init", "-q", "-b", "main")
	git(t, sub, "-c", "protocol.file.allow=always", "submodule", "add", "-q", filepath.ToSlash(nested), "nested")
	commitAll(t, sub)

	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", pkg("remote", ""))
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", filepath.ToSlash(sub), "sub")
	commitAll(t, repo)

	g, err := Resolve(context.Background(), write(t, dir+"/package.nfb.toml", pkg("a", gitDep(repo))), Options{})
	if err != nil {
		t.Fatal(err)
	}
	source := g.Units[0].SourceDir
	for file, want := range map[string]string{"sub/lib.txt": "lib\n", "sub/nested/deep.txt": "deep\n"} {
		got, err := os.ReadFile(filepath.Join(source, file))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s: got %q", file, got)
		}
	}
	for _, leftover := range []string{".git", "sub/.git", "sub/nested/.git"} {
		if exists(filepath.Join(source, leftover)) {
			t.Errorf("%s was not removed", leftover)
		}
	}
}

func TestSubmoduleCannotUseCommandTransport(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	marker := filepath.ToSlash(filepath.Join(dir, "pwned"))

	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", pkg("remote", ""))
	write(t, repo+"/.gitmodules", "[submodule \"evil\"]\n\tpath = evil\n\turl = ext::sh -c touch% "+marker+"\n")
	git(t, repo, "init", "-q", "-b", "main")
	commitAll(t, repo)
	git(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+git(t, repo, "rev-parse", "HEAD")+",evil")
	git(t, repo, "commit", "-qm", "gitlink")

	if _, err := Resolve(context.Background(), write(t, dir+"/package.nfb.toml", pkg("a", gitDep(repo))), Options{}); err == nil {
		t.Error("expected the ext submodule to fail")
	}
	if exists(marker) {
		t.Error("the ext transport ran a command")
	}
}

func TestGitRefCanBeAnOlderCommit(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", pkg("remote", ""))
	write(t, repo+"/version.txt", "one\n")
	git(t, repo, "init", "-q", "-b", "main")
	commitAll(t, repo)
	old := git(t, repo, "rev-parse", "HEAD")
	write(t, repo+"/version.txt", "two\n")
	commitAll(t, repo)

	dep := strings.Replace(gitDep(repo), `"main"`, `"`+old+`"`, 1)
	g, err := Resolve(context.Background(), write(t, dir+"/package.nfb.toml", pkg("a", dep)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(g.Units[0].SourceDir, "version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\n" {
		t.Errorf("checked out %q", got)
	}
}

func TestFetchedPackageCannotAskForSecrets(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", strings.Replace(pkg("remote", ""), `image = "x"`, "image = \"x\"\nsecrets = [\"AWS_KEY\"]", 1))
	git(t, repo, "init", "-q", "-b", "main")
	commitAll(t, repo)

	_, err := Resolve(context.Background(), write(t, dir+"/package.nfb.toml", pkg("a", gitDep(repo))), Options{})
	if err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Errorf("expected a secrets error, got %v", err)
	}
}

func TestHostTokenOnlyGoesToItsHostOverHTTPS(t *testing.T) {
	tempCache(t)
	var got string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(pkg("private", "")))
	}))
	defer server.Close()
	httpClient = server.Client()
	defer func() { httpClient = http.DefaultClient }()
	t.Setenv("NFB_TOKEN_127_0_0_1", "abc")

	dir := t.TempDir()
	root := write(t, dir+"/package.nfb.toml", pkg("a", "[[dependency]]\nsource = \"url\"\nurl = \""+server.URL+"/m.toml\"\n"))
	if _, err := Resolve(context.Background(), root, Options{}); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer abc" {
		t.Errorf("Authorization = %q", got)
	}

	insecure, err := url.Parse("http://127.0.0.1/m.toml")
	if err != nil {
		t.Fatal(err)
	}
	if hostToken(insecure) != "" {
		t.Error("token sent over plain http")
	}
	other, err := url.Parse("https://example.com/m.toml")
	if err != nil {
		t.Fatal(err)
	}
	if hostToken(other) != "" {
		t.Error("token sent to another host")
	}
}

func TestURLChecksums(t *testing.T) {
	tempCache(t)
	body := pkg("remote", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer server.Close()

	dir := t.TempDir()
	root := write(t, dir+"/package.nfb.toml", pkg("a", "[[dependency]]\nsource = \"url\"\nurl = \""+server.URL+"/m.toml\"\n"))

	g, err := Resolve(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	if got := g.Lock.Checksums[server.URL+"/m.toml"]; got != hex.EncodeToString(sum[:]) {
		t.Errorf("checksum not recorded: %s", got)
	}
	if err := g.SaveLock(); err != nil {
		t.Fatal(err)
	}

	body = pkg("changed", "")
	if _, err := Resolve(context.Background(), root, Options{NoCache: true}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected a checksum mismatch, got %v", err)
	}

	wrong := strings.Repeat("0", 64)
	bad := write(t, dir+"/bad.toml", pkg("b", "[[dependency]]\nsource = \"url\"\nurl = \""+server.URL+"/m.toml\"\nsha256 = \""+wrong+"\"\n"))
	if _, err := Resolve(context.Background(), bad, Options{}); err == nil {
		t.Error("expected a declared checksum mismatch to fail")
	}
}

func fastRetries(t *testing.T) {
	t.Helper()
	oldStall, oldDelay := stallTimeout, retryDelay
	stallTimeout, retryDelay = 100*time.Millisecond, time.Millisecond
	t.Cleanup(func() { stallTimeout, retryDelay = oldStall, oldDelay })
}

func urlProject(t *testing.T, serverURL string) string {
	t.Helper()
	return write(t, t.TempDir()+"/package.nfb.toml", pkg("a", "[[dependency]]\nsource = \"url\"\nurl = \""+serverURL+"/m.toml\"\n"))
}

func TestDownloadRetriesTemporaryFailures(t *testing.T) {
	tempCache(t)
	fastRetries(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(pkg("remote", "")))
	}))
	defer server.Close()

	if _, err := Resolve(context.Background(), urlProject(t, server.URL), Options{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("made %d attempts", calls.Load())
	}
}

func TestDownloadRestartsCleanlyAfterTruncation(t *testing.T) {
	tempCache(t)
	fastRetries(t)
	body := pkg("remote", "")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "100000")
			w.Write([]byte(body[:10]))
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()

	g, err := Resolve(context.Background(), urlProject(t, server.URL), Options{})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	if got := g.Lock.Checksums[server.URL+"/m.toml"]; got != hex.EncodeToString(sum[:]) {
		t.Errorf("the retry kept data from the failed attempt: %s", got)
	}
}

func TestDownloadDoesNotRetryNotFound(t *testing.T) {
	tempCache(t)
	fastRetries(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := Resolve(context.Background(), urlProject(t, server.URL), Options{}); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Errorf("made %d attempts", calls.Load())
	}
}

func TestDownloadGivesUpWhenItStalls(t *testing.T) {
	tempCache(t)
	fastRetries(t)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer server.Close()
	defer close(done)

	_, err := Resolve(context.Background(), urlProject(t, server.URL), Options{})
	if err == nil || !strings.Contains(err.Error(), "no data for") {
		t.Errorf("expected a stall error, got %v", err)
	}
}

func TestCancelStopsDownloadAndCleansUp(t *testing.T) {
	tempCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		cancel()
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer server.Close()
	defer close(done)

	if _, err := Resolve(ctx, urlProject(t, server.URL), Options{}); err == nil {
		t.Fatal("expected an error")
	}
	cache, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cache, "downloads"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("left %d files behind", len(entries))
	}
}

func TestParallelRunsShareTheCache(t *testing.T) {
	tempCache(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	write(t, repo+"/package.nfb.toml", pkg("gitpkg", ""))
	git(t, repo, "init", "-q", "-b", "main")
	commitAll(t, repo)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(pkg("urlpkg", "")))
	}))
	defer server.Close()
	deps := gitDep(repo) + "[[dependency]]\nsource = \"url\"\nurl = \"" + server.URL + "/m.toml\"\n"

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		root := write(t, filepath.Join(dir, "p"+strings.Repeat("x", i), "package.nfb.toml"), pkg("a", deps))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Resolve(context.Background(), root, Options{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestSweep(t *testing.T) {
	cache := t.TempDir()
	long := time.Now().Add(-2 * cacheMaxAge)
	recent := time.Now().Add(-2 * tempMaxAge)
	entries := map[string]struct {
		age  time.Time
		kept bool
	}{
		"build-old":        {recent, false},
		"build-new":        {time.Now(), true},
		"git/tmp-old":      {recent, false},
		"git/abc":          {recent, true},
		"git/unused":       {long, false},
		"downloads/tmp-x":  {recent, false},
		"downloads/abc":    {recent, true},
		"downloads/unused": {long, false},
	}
	for name, entry := range entries {
		file := write(t, filepath.Join(cache, name), "x")
		if err := os.Chtimes(file, entry.age, entry.age); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"git", "downloads"} {
		if err := os.Chtimes(filepath.Join(cache, dir), long, long); err != nil {
			t.Fatal(err)
		}
	}

	sweep(cache)
	for name, entry := range entries {
		if exists(filepath.Join(cache, name)) != entry.kept {
			t.Errorf("%s: expected kept=%v", name, entry.kept)
		}
	}
}

func TestSaveLockLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	g := &Graph{LockPath: filepath.Join(dir, "nfb.lock"), Lock: newLock()}
	if err := g.SaveLock(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("found %d files", len(entries))
	}
}

func TestCommandStopsChildrenOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows leaves children running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	Command(ctx, "cmd", "/c", "ping -n 30 127.0.0.1").CombinedOutput()
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %s to stop", took)
	}
}
