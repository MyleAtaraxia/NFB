// Package store holds what the nfb-packages repository needs to publish and review itself.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/MyleAtaraxia/nfb/internal/manifest"
	"github.com/MyleAtaraxia/nfb/internal/resolver"
	"path"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	packagesDir  = "packages"
	manifestFile = "package.nfb.toml"
	metaFile     = "meta.toml"
)

// Meta is a package's meta.toml, which describes every version of it.
type Meta struct {
	Description string   `toml:"description"`
	License     string   `toml:"license"`
	Homepage    string   `toml:"homepage"`
	Platforms   []string `toml:"platforms"`
	Yanked      []string `toml:"yanked"`
}

func parseMeta(data []byte) (*Meta, error) {
	var meta Meta
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&meta); err != nil {
		return nil, err
	}
	if len(meta.Platforms) == 0 {
		meta.Platforms = []string{"linux/amd64"}
	}
	return &meta, nil
}

func git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := resolver.Command(ctx, "git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func lines(out []byte) []string {
	return strings.Fields(string(out))
}

func manifestPath(name string) string { return path.Join(packagesDir, name, manifestFile) }

func show(ctx context.Context, repo, rev, file string) ([]byte, error) {
	return git(ctx, repo, "show", rev+":"+file)
}

// Index builds the store's metadata from git history: each commit that introduces a version
// is where that version lives, and the version added last is the latest.
func Index(ctx context.Context, repo, rev, rawBase string) (*resolver.Store, error) {
	out, err := git(ctx, repo, "ls-tree", "-d", "--name-only", rev, packagesDir+"/")
	if err != nil {
		return nil, err
	}
	store := &resolver.Store{Schema: resolver.StoreSchema, Packages: map[string]*resolver.StorePackage{}}
	for _, dir := range lines(out) {
		name := path.Base(dir)
		data, err := show(ctx, repo, rev, path.Join(dir, metaFile))
		if err != nil {
			return nil, fmt.Errorf("%s: no %s", name, metaFile)
		}
		meta, err := parseMeta(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", name, metaFile, err)
		}
		p := &resolver.StorePackage{
			Description: meta.Description,
			License:     meta.License,
			Homepage:    meta.Homepage,
			Platforms:   meta.Platforms,
			Versions:    map[string]*resolver.StoreVersion{},
		}
		log, err := git(ctx, repo, "log", "--reverse", "--topo-order", "--diff-filter=AM", "--format=%H", rev, "--", manifestPath(name))
		if err != nil {
			return nil, err
		}
		for _, commit := range lines(log) {
			data, err := show(ctx, repo, commit, manifestPath(name))
			if err != nil {
				return nil, err
			}
			// Only the identity is read, so old versions survive changes to the manifest format.
			var id struct {
				Package manifest.Identity `toml:"package"`
			}
			if err := toml.Unmarshal(data, &id); err != nil {
				return nil, fmt.Errorf("%s at %s: %w", name, commit, err)
			}
			version := id.Package.Version
			p.Dynamic = manifest.Dynamic(data)
			if _, seen := p.Versions[version]; (seen && !p.Dynamic) || version == "" {
				continue
			}
			sum := sha256.Sum256(data)
			yanked := slices.Contains(meta.Yanked, version)
			p.Versions[version] = &resolver.StoreVersion{
				URL:    strings.TrimRight(rawBase, "/") + "/" + commit + "/" + manifestPath(name),
				SHA256: hex.EncodeToString(sum[:]),
				Yanked: yanked,
			}
			if !yanked {
				p.Latest = version
			}
		}
		if latest, ok := p.Versions[p.Latest]; ok && p.Dynamic {
			for _, version := range meta.Yanked {
				if _, ok := p.Versions[version]; !ok {
					yanked := *latest
					yanked.Yanked = true
					p.Versions[version] = &yanked
				}
			}
		}
		store.Packages[name] = p
	}
	return store, nil
}

// Check reviews what a change does to the store and returns the packages it touches.
func Check(ctx context.Context, repo, base string) ([]string, error) {
	out, err := git(ctx, repo, "diff", "--name-only", base+"...HEAD", "--", packagesDir)
	if err != nil {
		return nil, err
	}
	var names []string
	var errs []error
	for _, file := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if file == "" {
			continue
		}
		parts := strings.Split(file, "/")
		if len(parts) != 3 || (parts[2] != manifestFile && parts[2] != metaFile) {
			errs = append(errs, fmt.Errorf("%s: a package can only hold %s and %s", file, manifestFile, metaFile))
			continue
		}
		if !slices.Contains(names, parts[1]) {
			names = append(names, parts[1])
		}
	}

	head, err := Index(ctx, repo, "HEAD", "")
	if err != nil {
		return nil, err
	}
	old, err := Index(ctx, repo, base, "")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := checkPackage(ctx, repo, name, head, old); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return names, errors.Join(errs...)
}

func checkPackage(ctx context.Context, repo, name string, head, old *resolver.Store) error {
	if !manifest.StoreName(name) {
		return errors.New("a store name can only use lowercase letters, numbers and -")
	}
	data, err := show(ctx, repo, "HEAD", manifestPath(name))
	if err != nil {
		return errors.New("packages can't be deleted, add broken versions to yanked in meta.toml")
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	if m.Package == nil {
		return errors.New("a store manifest must be a package")
	}
	if m.Package.Name != name {
		return fmt.Errorf("[package] name is %q, it must match the folder", m.Package.Name)
	}
	if !manifest.StoreVersion(m.Package.Version) {
		return fmt.Errorf("invalid version %q", m.Package.Version)
	}

	metaData, err := show(ctx, repo, "HEAD", path.Join(packagesDir, name, metaFile))
	if err != nil {
		return fmt.Errorf("no %s", metaFile)
	}
	meta, err := parseMeta(metaData)
	if err != nil {
		return fmt.Errorf("%s: %w", metaFile, err)
	}
	if meta.Description == "" {
		return fmt.Errorf("%s needs a description", metaFile)
	}

	if m.Source != nil && m.Source.SHA256 == "" {
		return errors.New("[source] needs a sha256, run nfb pin")
	}
	for _, dep := range m.Dependencies {
		switch dep.Source {
		case "cmd":
		case "store":
			if _, err := head.Lookup(dep.Name, dep.Version); err != nil {
				return err
			}
		default:
			return fmt.Errorf("source %q isn't allowed, a store package can only use cmd and store", dep.Source)
		}
	}

	sum := sha256.Sum256(data)
	if p, ok := old.Packages[name]; ok {
		if v, ok := p.Versions[m.Package.Version]; ok && v.SHA256 != hex.EncodeToString(sum[:]) && !manifest.Dynamic(data) {
			return fmt.Errorf("version %s is already published, a change needs a new version", m.Package.Version)
		}
	}
	for _, version := range meta.Yanked {
		if _, ok := head.Packages[name].Versions[version]; !ok {
			return fmt.Errorf("yanked version %q doesn't exist", version)
		}
	}
	return nil
}
