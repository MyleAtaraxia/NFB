package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const meta = "description = \"a tool\"\n"

func manifestFor(name, version, extra string) string {
	return "[package]\nname = \"" + name + "\"\nversion = \"" + version + "\"\n" + extra + "[build]\nimage = \"x\"\n"
}

type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t, t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write("README.md", "store")
	r.commit()
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "core.autocrlf=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(file, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) publish(name, version, extra string) string {
	r.t.Helper()
	r.write("packages/"+name+"/package.nfb.toml", manifestFor(name, version, extra))
	r.write("packages/"+name+"/meta.toml", meta)
	return r.commit()
}

func (r *repo) commit() string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "change")
	return r.git("rev-parse", "HEAD")
}

func TestIndexFindsEveryVersionInHistory(t *testing.T) {
	r := newRepo(t)
	first := r.publish("llvm", "18.0.0", "")
	r.publish("other", "1", "")
	second := r.publish("llvm", "19.1.0", "")

	s, err := Index(context.Background(), r.dir, "HEAD", "https://raw.example/store/")
	if err != nil {
		t.Fatal(err)
	}
	p := s.Packages["llvm"]
	if p.Latest != "19.1.0" || len(p.Versions) != 2 || p.Description != "a tool" {
		t.Fatalf("package: %+v", p)
	}
	if got, want := p.Versions["18.0.0"].URL, "https://raw.example/store/"+first+"/packages/llvm/package.nfb.toml"; got != want {
		t.Errorf("url %s", got)
	}
	if !strings.Contains(p.Versions["19.1.0"].URL, second) {
		t.Errorf("url %s", p.Versions["19.1.0"].URL)
	}
	if got := p.Platforms; len(got) != 1 || got[0] != "linux/amd64" {
		t.Errorf("platforms %v", got)
	}
}

func TestYankedVersionsStayButAreNotLatest(t *testing.T) {
	r := newRepo(t)
	r.publish("llvm", "1", "")
	r.publish("llvm", "2", "")
	r.write("packages/llvm/meta.toml", meta+"yanked = [\"2\"]\n")
	r.commit()

	s, err := Index(context.Background(), r.dir, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	p := s.Packages["llvm"]
	if p.Latest != "1" || !p.Versions["2"].Yanked {
		t.Errorf("package: %+v, %+v", p, p.Versions["2"])
	}
}

func TestLatestIsTheLastVersionAdded(t *testing.T) {
	r := newRepo(t)
	r.publish("llvm", "2", "")
	r.publish("llvm", "1", "")
	s, err := Index(context.Background(), r.dir, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Packages["llvm"].Latest != "1" {
		t.Errorf("latest %s", s.Packages["llvm"].Latest)
	}
}

func TestCheckAcceptsAGoodChange(t *testing.T) {
	r := newRepo(t)
	r.publish("dep", "1", "")
	base := r.git("rev-parse", "HEAD")
	sha := strings.Repeat("a", 64)
	r.publish("llvm", "1", "[source]\nurl = \"https://x.dev/a.tar.gz\"\nsha256 = \""+sha+"\"\n[[dependency]]\nsource = \"store\"\nname = \"dep\"\nversion = \"1\"\n[[dependency]]\nsource = \"cmd\"\ncommand = [\"apt-get update\"]\n")

	names, err := Check(context.Background(), r.dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "llvm" {
		t.Errorf("names %v", names)
	}
}

func TestCheckRejects(t *testing.T) {
	const noSum = "[source]\nurl = \"https://x.dev/a.tar.gz\"\n"
	depOn := func(body string) string { return "[[dependency]]\n" + body }
	cases := map[string]struct {
		change func(r *repo)
		want   string
	}{
		"no checksum": {func(r *repo) { r.publish("new", "1", noSum) }, "nfb pin"},
		"edited version": {func(r *repo) {
			r.write("packages/llvm/package.nfb.toml", manifestFor("llvm", "1", "[[dependency]]\nsource = \"cmd\"\ncommand = [\"curl evil\"]\n"))
			r.commit()
		}, "already published"},
		"wrong folder": {func(r *repo) {
			r.write("packages/new/package.nfb.toml", manifestFor("other", "1", ""))
			r.write("packages/new/meta.toml", meta)
			r.commit()
		}, "must match the folder"},
		"capital name": {func(r *repo) { r.publish("New", "1", "") }, "lowercase"},
		"no meta": {func(r *repo) {
			r.write("packages/new/package.nfb.toml", manifestFor("new", "1", ""))
			r.commit()
		}, "meta.toml"},
		"extra file": {func(r *repo) {
			r.publish("new", "1", "")
			r.write("packages/new/fix.patch", "x")
			r.commit()
		}, "can only hold"},
		"local dep": {func(r *repo) {
			r.publish("new", "1", depOn("source = \"local\"\npath = \"../llvm/package.nfb.toml\"\n"))
		}, "isn't allowed"},
		"git dep": {func(r *repo) {
			r.publish("new", "1", depOn("source = \"git\"\nurl = \"https://x.dev/r\"\nref = \"main\"\n"))
		}, "isn't allowed"},
		"missing dep": {func(r *repo) {
			r.publish("new", "1", depOn("source = \"store\"\nname = \"nope\"\nversion = \"1\"\n"))
		}, "no package"},
		"deleted package": {func(r *repo) {
			r.git("rm", "-q", "-r", "packages/llvm")
			r.commit()
		}, "can't be deleted"},
		"unknown yank": {func(r *repo) {
			r.write("packages/llvm/meta.toml", meta+"yanked = [\"9\"]\n")
			r.commit()
		}, "doesn't exist"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			r.publish("llvm", "1", "")
			base := r.git("rev-parse", "HEAD")
			c.change(r)
			_, err := Check(context.Background(), r.dir, base)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected %q, got %v", c.want, err)
			}
		})
	}
}
