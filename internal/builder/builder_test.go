package builder

import (
	"archive/zip"
	"context"
	"github.com/MyleAtaraxia/NFB/internal/manifest"
	"github.com/MyleAtaraxia/NFB/internal/resolver"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptOrderAndShell(t *testing.T) {
	dep := &resolver.Unit{Manifest: &manifest.Manifest{
		Package: &manifest.Identity{Name: "tool", Version: "1"},
		Init:    &manifest.Init{Commands: []string{"export TOOL=$PWD"}},
	}}
	u := &resolver.Unit{Deps: []*resolver.Unit{dep}, Manifest: &manifest.Manifest{
		Package:      &manifest.Identity{Name: "app", Version: "1"},
		Build:        manifest.Build{Env: map[string]string{"A": "it's"}, Commands: []string{"make"}},
		Dependencies: []manifest.Dependency{{Source: "cmd", Commands: []string{"apt install x"}}},
		Output:       &manifest.Output{Artifacts: []manifest.Artifact{{From: "bin/", To: "."}}},
	}}

	got := script(u)
	want := []string{"set -e", `export A='it'\''s'`, "apt install x", "cd '/nfb/deps/tool'", "export TOOL=$PWD", "cd /nfb/src", "make", "[ -d 'bin/' ]"}
	pos := 0
	for _, line := range want {
		i := strings.Index(got[pos:], line)
		if i < 0 {
			t.Fatalf("missing or out of order %q in:\n%s", line, got)
		}
		pos += i
	}
}

func TestDependencyInitNotRunInOwnBuild(t *testing.T) {
	u := &resolver.Unit{Manifest: &manifest.Manifest{
		Package: &manifest.Identity{Name: "pkg", Version: "1"},
		Init:    &manifest.Init{Commands: []string{"export OWN=1"}},
	}}
	if strings.Contains(script(u), "OWN") {
		t.Error("a package's [init] ran in its own build")
	}
}

func TestGenerateDockerfile(t *testing.T) {
	dep := &resolver.Unit{Manifest: &manifest.Manifest{
		Package: &manifest.Identity{Name: "tool", Version: "1"},
		Build:   manifest.Build{Image: "alpine:3"},
	}}
	root := &resolver.Unit{SourceDir: "/src", Deps: []*resolver.Unit{dep}, Manifest: &manifest.Manifest{
		Project: &manifest.Identity{Name: "app"},
		Build:   manifest.Build{Image: "debian:12", Secrets: []string{"TOKEN"}},
		Output:  &manifest.Output{Artifacts: []manifest.Artifact{{From: "bin/", To: "out/"}, {From: "/usr/bin/x", To: "."}}},
	}}
	g := &resolver.Graph{Units: []*resolver.Unit{dep, root}, Images: map[string]string{"debian:12": "debian:12@sha256:abc"}}

	gen := t.TempDir()
	contexts, err := generate(g, gen, "/src/dist")
	if err != nil {
		t.Fatal(err)
	}
	if contexts["nfb_src1"] != "/src" {
		t.Errorf("contexts: %v", contexts)
	}
	data, err := os.ReadFile(filepath.Join(gen, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"FROM alpine:3 AS nfb_u0",
		"FROM debian:12@sha256:abc AS nfb_u1",
		"COPY --from=nfb_u0_out / /nfb/deps/tool/",
		"COPY --exclude=dist --from=nfb_src1 . ./",
		"RUN --mount=type=cache,id=nfb-cache-app,target=/nfb/cache,sharing=locked --mount=type=secret,id=NFB_SECRET_1_TOKEN,env=TOKEN [",
		`COPY --from=nfb_u1 ["/nfb/src/bin/","/out/"]`,
		`COPY --from=nfb_u1 ["/usr/bin/x","/x"]`,
	} {
		if !strings.Contains(string(data), line) {
			t.Errorf("missing %q in:\n%s", line, data)
		}
	}
}

func TestExcludeOutputSkipsWhatItCannotExclude(t *testing.T) {
	for _, out := range []string{"/src", "/elsewhere/dist", "/src/my dist"} {
		if got := excludeOutput("/src", out); got != "" {
			t.Errorf("%s: got %q", out, got)
		}
	}
}

func TestUnzipRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	build := func(name string) string {
		file := filepath.Join(dir, "a.zip")
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		w := zip.NewWriter(f)
		defer w.Close()
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
		return file
	}
	if err := unzip(build("../evil"), filepath.Join(dir, "out")); err == nil {
		t.Error("expected an escaping entry to fail")
	}
}

func TestInterruptedBuildCleansUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LocalAppData", dir)
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	root := &resolver.Unit{Manifest: &manifest.Manifest{
		Project: &manifest.Identity{Name: "app"},
		Build:   manifest.Build{Image: "alpine:3"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Build(ctx, &resolver.Graph{Units: []*resolver.Unit{root}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("expected an interrupted error, got %v", err)
	}
	cache, err := resolver.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("left %d entries behind", len(entries))
	}
}

func TestSecretsStayWithTheirUnit(t *testing.T) {
	t.Setenv("TOKEN", "real")
	dep := &resolver.Unit{Remote: true, Depth: 1, Manifest: &manifest.Manifest{
		Package: &manifest.Identity{Name: "dep", Version: "1"},
		Build:   manifest.Build{Secrets: []string{"TOKEN"}, SecretDefaults: map[string]string{"TOKEN": "fallback"}},
	}}
	root := &resolver.Unit{Manifest: &manifest.Manifest{
		Project: &manifest.Identity{Name: "app"},
		Build:   manifest.Build{Secrets: []string{"TOKEN"}},
	}}
	g := &resolver.Graph{Units: []*resolver.Unit{dep, root}, Lock: &resolver.Lock{}}

	_, values, err := hostSecrets(g)
	if err != nil {
		t.Fatal(err)
	}
	if want := "NFB_SECRET_0_TOKEN_DEFAULT=fallback,NFB_SECRET_1_TOKEN=real"; strings.Join(values, ",") != want {
		t.Errorf("got %v", values)
	}
	if !strings.Contains(mounts(g, 0, dep), "id=NFB_SECRET_0_TOKEN_DEFAULT,env=TOKEN") {
		t.Errorf("a default must not share a cached layer with the real secret: %s", mounts(g, 0, dep))
	}
}

func TestSecretErrors(t *testing.T) {
	unit := func(remote bool) *resolver.Graph {
		u := &resolver.Unit{Remote: remote, Depth: 1, Manifest: &manifest.Manifest{
			Package: &manifest.Identity{Name: "dep", Version: "1"},
			Build:   manifest.Build{Secrets: []string{"NFB_TEST_UNSET"}},
		}}
		return &resolver.Graph{Units: []*resolver.Unit{u}, Lock: &resolver.Lock{}}
	}
	if _, _, err := hostSecrets(unit(true)); err == nil || !strings.Contains(err.Error(), "approved") {
		t.Errorf("got %v", err)
	}
	if _, _, err := hostSecrets(unit(false)); err == nil || !strings.Contains(err.Error(), "isn't set") {
		t.Errorf("got %v", err)
	}
	nested := unit(true)
	nested.Units[0].Depth = 2
	if _, _, err := hostSecrets(nested); err == nil || !strings.Contains(err.Error(), "direct") {
		t.Errorf("got %v", err)
	}
}
