package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MyleAtaraxia/nfb/internal/manifest"
	"os"
)

const (
	defaultStore = "https://myleataraxia.github.io/nfb-packages/metadata.json"
	storeSchema  = 1
)

// Store is the package store's metadata.json.
type Store struct {
	Schema   int                      `json:"schema"`
	Packages map[string]*StorePackage `json:"packages"`
}

type StorePackage struct {
	Description string                   `json:"description"`
	License     string                   `json:"license,omitempty"`
	Homepage    string                   `json:"homepage,omitempty"`
	Platforms   []string                 `json:"platforms"`
	Latest      string                   `json:"latest,omitempty"`
	Versions    map[string]*StoreVersion `json:"versions"`
}

type StoreVersion struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Yanked bool   `json:"yanked,omitempty"`
}

// offline is a resolver with no project attached, for fetching outside of a build.
func offline(ctx context.Context) (*resolver, error) {
	cache, err := CacheDir()
	if err != nil {
		return nil, err
	}
	return &resolver{ctx: ctx, cache: cache, graph: &Graph{Lock: newLock(), oldLock: newLock()}}, nil
}

// FetchStore always downloads the metadata, because it changes on every merge to the store.
func FetchStore(ctx context.Context) (*Store, error) {
	r, err := offline(ctx)
	if err != nil {
		return nil, err
	}
	url := os.Getenv("NFB_STORE")
	if url == "" {
		url = defaultStore
	}
	file, _, err := r.download(url, "")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if store.Schema != storeSchema {
		return nil, fmt.Errorf("%s has schema %d, this version of NFB reads %d", url, store.Schema, storeSchema)
	}
	return &store, nil
}

// Checksum returns the sha256 of a file at a URL.
func Checksum(ctx context.Context, url string) (string, error) {
	r, err := offline(ctx)
	if err != nil {
		return "", err
	}
	_, sum, err := r.download(url, "")
	return sum, err
}

func (s *Store) version(name, version string) (*StoreVersion, error) {
	p, ok := s.Packages[name]
	if !ok {
		return nil, fmt.Errorf("the store has no package %q", name)
	}
	v, ok := p.Versions[version]
	if !ok {
		return nil, fmt.Errorf("the store has no version %q of %s", version, name)
	}
	return v, nil
}

// Lookup finds a version a project can start using. An empty version means the latest.
func (s *Store) Lookup(name, version string) (string, error) {
	if p, ok := s.Packages[name]; ok && version == "" {
		version = p.Latest
		if version == "" {
			return "", fmt.Errorf("every version of %s has been yanked", name)
		}
	}
	v, err := s.version(name, version)
	if err != nil {
		return "", err
	}
	if v.Yanked {
		return "", fmt.Errorf("%s %s has been yanked", name, version)
	}
	return version, nil
}

func (r *resolver) storeDependency(dep manifest.Dependency) (*Unit, error) {
	id := dep.Name + "@" + dep.Version
	url := r.graph.oldLock.Store[id]
	sum := ""
	if url == "" {
		if r.store == nil {
			store, err := FetchStore(r.ctx)
			if err != nil {
				return nil, err
			}
			r.store = store
		}
		v, err := r.store.version(dep.Name, dep.Version)
		if err != nil {
			return nil, err
		}
		if v.Yanked {
			fmt.Fprintln(os.Stderr, "warning:", dep.Name, dep.Version, "has been yanked")
		}
		url, sum = v.URL, v.SHA256
	}

	file, _, err := r.download(url, sum)
	if err != nil {
		return nil, err
	}
	m, err := loadPackage(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	if m.Package.Name != dep.Name || m.Package.Version != dep.Version {
		return nil, fmt.Errorf("%s: the store returned %s@%s", id, m.Package.Name, m.Package.Version)
	}
	r.graph.Lock.Store[id] = url
	return r.unit("store:"+id, m, "", true)
}
