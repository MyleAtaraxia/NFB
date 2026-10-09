package resolver

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	g, err := Resolve(root, false)
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
		if _, err := Resolve(root, false); err == nil {
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

	g, err := Resolve(root, false)
	if err != nil {
		t.Fatal(err)
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

	again, err := Resolve(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Lock.Git[filepath.ToSlash(repo)+"#main"]; got != first {
		t.Errorf("lock moved to %s, want %s", got, first)
	}

	withPath := dep + "path = \"nested/evil.toml\"\n"
	if _, err := Resolve(write(t, dir+"/evil.toml", pkg("b", withPath)), false); err == nil {
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

	g, err := Resolve(write(t, dir+"/package.nfb.toml", pkg("a", gitDep(repo))), false)
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

	if _, err := Resolve(write(t, dir+"/package.nfb.toml", pkg("a", gitDep(repo))), false); err == nil {
		t.Error("expected the ext submodule to fail")
	}
	if exists(marker) {
		t.Error("the ext transport ran a command")
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

	g, err := Resolve(root, false)
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
	if _, err := Resolve(root, true); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected a checksum mismatch, got %v", err)
	}

	wrong := strings.Repeat("0", 64)
	bad := write(t, dir+"/bad.toml", pkg("b", "[[dependency]]\nsource = \"url\"\nurl = \""+server.URL+"/m.toml\"\nsha256 = \""+wrong+"\"\n"))
	if _, err := Resolve(bad, false); err == nil {
		t.Error("expected a declared checksum mismatch to fail")
	}
}
