package builder

import (
	"NFB/internal/manifest"
	"NFB/internal/resolver"
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	srcDir   = "/nfb/src"
	depsDir  = "/nfb/deps"
	cacheDir = "/nfb/cache"
)

var excludablePath = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type Options struct {
	OutputOverride string
	NoCache        bool
	Verbose        bool
}

// Build exports the root unit's artifacts and returns where they went.
func Build(g *resolver.Graph, opts Options) (string, error) {
	cache, err := resolver.CacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	gen, err := os.MkdirTemp(cache, "build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(gen)

	out, err := outputDir(g, opts.OutputOverride)
	if err != nil {
		return "", err
	}
	contexts, err := generate(g, gen, out)
	if err != nil {
		return "", err
	}

	args := []string{"build", "-f", filepath.Join(gen, "Dockerfile"), "--target", outStage(len(g.Units) - 1), "--output", "type=local,dest=" + out}
	for name, dir := range contexts {
		args = append(args, "--build-context", name+"="+dir)
	}
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	secrets, err := hostSecrets(g)
	if err != nil {
		return "", err
	}
	args = append(args, secrets...)

	cmd := exec.Command("docker", append(append(args, "--progress=plain"), gen)...)
	cmd.Env = append(cmd.Environ(), "DOCKER_BUILDKIT=1")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if opts.Verbose {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	fmt.Fprintln(os.Stderr, "building", g.Root().Name())
	if err := cmd.Run(); err != nil {
		os.Stderr.Write(logs.Bytes())
		return "", fmt.Errorf("docker build: %w", err)
	}
	return out, nil
}

func hostSecrets(g *resolver.Graph) ([]string, error) {
	var args []string
	for _, u := range g.Units {
		for _, name := range u.Manifest.Build.Secrets {
			if _, ok := os.LookupEnv(name); !ok {
				return nil, fmt.Errorf("%s needs the secret %s, but it isn't set", u.Name(), name)
			}
			if flag := "id=" + name + ",env=" + name; !slices.Contains(args, flag) {
				args = append(args, "--secret", flag)
			}
		}
	}
	return args, nil
}

func outputDir(g *resolver.Graph, override string) (string, error) {
	m := g.Root().Manifest
	dir := override
	if dir == "" {
		dir = "dist"
		if m.Output != nil && m.Output.Dir != "" {
			dir = m.Output.Dir
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(g.Root().SourceDir, dir)
		}
	}
	if m.Package != nil {
		dir = filepath.Join(dir, m.Package.Name)
	}
	return filepath.Abs(dir)
}

func stage(i int) string      { return fmt.Sprintf("nfb_u%d", i) }
func outStage(i int) string   { return fmt.Sprintf("nfb_u%d_out", i) }
func srcContext(i int) string { return fmt.Sprintf("nfb_src%d", i) }

// generate writes the Dockerfile and scripts into gen and returns the extra build contexts.
func generate(g *resolver.Graph, gen, out string) (map[string]string, error) {
	contexts := map[string]string{}
	index := map[*resolver.Unit]int{}
	var df strings.Builder

	for i, u := range g.Units {
		index[u] = i
		m := u.Manifest
		dir := fmt.Sprintf("u%d", i)
		if err := os.MkdirAll(filepath.Join(gen, dir), 0o755); err != nil {
			return nil, err
		}

		image := g.Images[m.Build.Image]
		if image == "" {
			image = m.Build.Image
		}
		fmt.Fprintf(&df, "FROM %s AS %s\nUSER root\nWORKDIR %s\n", image, stage(i), srcDir)

		if u.SourceDir != "" {
			contexts[srcContext(i)] = u.SourceDir
			exclude := ""
			if u == g.Root() {
				exclude = excludeOutput(u.SourceDir, out)
			}
			fmt.Fprintf(&df, "COPY %s--from=%s . ./\n", exclude, srcContext(i))
		}
		if u.Archive != "" {
			line, err := addArchive(u, gen, dir)
			if err != nil {
				return nil, err
			}
			df.WriteString(line)
		}
		for _, dep := range u.Deps {
			fmt.Fprintf(&df, "COPY --from=%s / %s/%s/\n", outStage(index[dep]), depsDir, dep.Name())
		}

		if err := os.WriteFile(filepath.Join(gen, dir, "run.sh"), []byte(script(u)), 0o644); err != nil {
			return nil, err
		}
		fmt.Fprintf(&df, "COPY %s/run.sh /nfb/run.sh\nRUN %s [\"/bin/sh\", \"/nfb/run.sh\"]\n", dir, mounts(u))

		fmt.Fprintf(&df, "FROM scratch AS %s\n", outStage(i))
		for _, a := range m.Artifacts() {
			from := a.From
			if !path.IsAbs(from) {
				from = path.Join(srcDir, from)
				if strings.HasSuffix(a.From, "/") {
					from += "/"
				}
			}
			paths, err := json.Marshal([]string{from, a.Dest()})
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&df, "COPY --from=%s %s\n", stage(i), paths)
		}
	}

	return contexts, os.WriteFile(filepath.Join(gen, "Dockerfile"), []byte(df.String()), 0o644)
}

func mounts(u *resolver.Unit) string {
	flags := []string{fmt.Sprintf("--mount=type=cache,id=nfb-cache-%s,target=%s,sharing=locked", u.Name(), cacheDir)}
	for _, name := range u.Manifest.Build.Secrets {
		flags = append(flags, fmt.Sprintf("--mount=type=secret,id=%s,env=%s", name, name))
	}
	return strings.Join(flags, " ")
}

// excludeOutput keeps earlier artifacts out of the source, where they would defeat the cache.
func excludeOutput(sourceDir, out string) string {
	rel, err := filepath.Rel(sourceDir, out)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if !excludablePath.MatchString(rel) {
		return ""
	}
	return "--exclude=" + rel + " "
}

// addArchive relies on ADD to unpack tars, which it only does for files in the main context.
func addArchive(u *resolver.Unit, gen, dir string) (string, error) {
	if manifest.ArchiveExt(u.Manifest.Source.URL) == ".zip" {
		if err := unzip(u.Archive, filepath.Join(gen, dir, "zip")); err != nil {
			return "", err
		}
		return fmt.Sprintf("COPY %s/zip/ ./\n", dir), nil
	}
	if err := os.Link(u.Archive, filepath.Join(gen, dir, "source.tar")); err != nil {
		return "", err
	}
	return fmt.Sprintf("ADD %s/source.tar ./\n", dir), nil
}

// shortcut: zip symlinks unpack as plain files, use a tar source if that matters.
func unzip(archive, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("zip entry %q escapes the archive", f.Name)
		}
		target := filepath.Join(dest, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extract(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

// script is everything a unit runs in its one shell, which also makes it the build's cache key.
func script(u *resolver.Unit) string {
	m := u.Manifest
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "export NFB_CACHE=%s\n", cacheDir)

	for _, key := range slices.Sorted(maps.Keys(m.Build.Env)) {
		fmt.Fprintf(&b, "export %s=%s\n", key, quote(m.Build.Env[key]))
	}
	for _, dep := range m.Dependencies {
		if dep.Source == "cmd" {
			writeLines(&b, dep.Commands)
		}
	}
	for _, dep := range u.Deps {
		if dep.Manifest.Init == nil {
			continue
		}
		root := quote(depsDir + "/" + dep.Name())
		fmt.Fprintf(&b, "mkdir -p %s\ncd %s\n", root, root)
		writeLines(&b, dep.Manifest.Init.Commands)
		fmt.Fprintf(&b, "cd %s\n", srcDir)
	}
	writeLines(&b, m.Build.Commands)

	fmt.Fprintf(&b, "cd %s\n", srcDir)
	for _, a := range m.Artifacts() {
		test := "[ -e %[1]s ] || [ -L %[1]s ]"
		if strings.HasSuffix(a.From, "/") {
			test = "[ -d %[1]s ]"
		}
		fmt.Fprintf(&b, test+" || { echo %[2]s >&2; exit 1; }\n", quote(a.From), quote("artifact not found: "+a.From))
	}
	return b.String()
}

func writeLines(b *strings.Builder, lines []string) {
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
