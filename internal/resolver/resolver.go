package resolver

import (
	"NFB/internal/manifest"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const defaultPackageFile = "package.nfb.toml"

// Unit is one manifest to build, with its source already on disk.
type Unit struct {
	Manifest  *manifest.Manifest
	SourceDir string
	Archive   string
	Deps      []*Unit
}

func (u *Unit) Name() string {
	return u.Manifest.Identity().Name
}

// Graph holds the units in build order, dependencies first. The root is last.
type Graph struct {
	Units    []*Unit
	LockPath string
	Lock     *Lock
	Images   map[string]string
	noCache  bool
	oldLock  *Lock
}

func (g *Graph) Root() *Unit {
	return g.Units[len(g.Units)-1]
}

type resolver struct {
	graph *Graph
	cache string
	units map[string]*Unit
	stack []string
}

func CacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nfb"), nil
}

// Options changes how Resolve treats what is already pinned or cached.
type Options struct {
	NoCache bool
	Update  bool
}

func Resolve(manifestPath string, opts Options) (*Graph, error) {
	root, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(root)
	if err != nil {
		return nil, err
	}
	cache, err := CacheDir()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(filepath.Dir(root), "nfb.lock")
	oldLock, err := ReadLock(lockPath)
	if err != nil {
		return nil, err
	}
	if opts.Update {
		oldLock = newLock()
	}

	r := &resolver{
		graph: &Graph{LockPath: lockPath, Lock: newLock(), Images: map[string]string{}, noCache: opts.NoCache, oldLock: oldLock},
		cache: cache,
		units: map[string]*Unit{},
	}
	if _, err := r.unit("root", m, filepath.Dir(root), false); err != nil {
		return nil, err
	}
	return r.graph, nil
}

func (r *resolver) unit(key string, m *manifest.Manifest, dir string, remote bool) (*Unit, error) {
	if u, ok := r.units[key]; ok {
		return u, nil
	}

	id := m.Identity().Name + "@" + m.Identity().Version
	if slices.Contains(r.stack, id) {
		return nil, fmt.Errorf("dependency cycle: %s -> %s", strings.Join(r.stack, " -> "), id)
	}
	if remote && len(m.Build.Secrets) > 0 {
		return nil, fmt.Errorf("%s: a fetched package cannot ask for secrets", id)
	}
	r.stack = append(r.stack, id)
	defer func() { r.stack = r.stack[:len(r.stack)-1] }()

	u := &Unit{Manifest: m, SourceDir: dir}
	for _, dep := range m.Dependencies {
		if dep.Source == "cmd" {
			continue
		}
		d, err := r.dependency(dep, dir, remote)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if slices.Contains(u.Deps, d) {
			continue
		}
		if slices.ContainsFunc(u.Deps, func(other *Unit) bool { return other.Name() == d.Name() }) {
			return nil, fmt.Errorf("%s: imports two versions of package %q", id, d.Name())
		}
		u.Deps = append(u.Deps, d)
	}

	if m.Source != nil {
		file, _, err := r.download(m.Source.URL, m.Source.SHA256)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		u.Archive = file
	}

	r.units[key] = u
	r.graph.Units = append(r.graph.Units, u)
	return u, nil
}

func (r *resolver) dependency(dep manifest.Dependency, dir string, remote bool) (*Unit, error) {
	switch dep.Source {
	case "local":
		if remote {
			return nil, fmt.Errorf("source \"local\" (%s) is not allowed in a fetched package", dep.Path)
		}
		file := filepath.FromSlash(dep.Path)
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		m, err := loadPackage(file)
		if err != nil {
			return nil, err
		}
		return r.unit("local:"+file, m, filepath.Dir(file), false)

	case "git":
		repo, commit, err := r.checkout(dep.URL, dep.Ref, remote)
		if err != nil {
			return nil, err
		}
		rel := dep.Path
		if rel == "" {
			rel = defaultPackageFile
		}
		file, err := insideRepo(repo, filepath.FromSlash(rel))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dep.URL, err)
		}
		m, err := loadPackage(file)
		if err != nil {
			return nil, err
		}
		return r.unit("git:"+dep.URL+"@"+commit+":"+rel, m, repo, true)

	case "url":
		file, sum, err := r.download(dep.URL, dep.SHA256)
		if err != nil {
			return nil, err
		}
		m, err := loadPackage(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dep.URL, err)
		}
		return r.unit("url:"+sum, m, "", true)
	}
	return nil, fmt.Errorf("unknown source %q", dep.Source)
}

func loadPackage(file string) (*manifest.Manifest, error) {
	m, err := manifest.Load(file)
	if err != nil {
		return nil, err
	}
	if m.Package == nil {
		return nil, fmt.Errorf("%s: a dependency must be a package, not a project", file)
	}
	return m, nil
}

// insideRepo follows symlinks, because a repository can link its manifest anywhere on the machine.
func insideRepo(repo, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	file, err := filepath.EvalSymlinks(filepath.Join(repo, rel))
	if err != nil {
		return "", err
	}
	inner, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(inner) {
		return "", errors.New("path escapes the repository")
	}
	return file, nil
}
