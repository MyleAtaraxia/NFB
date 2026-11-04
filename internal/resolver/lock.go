package resolver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Lock pins what a manifest leaves floating.
type Lock struct {
	Git       map[string]string `toml:"git"`
	Checksums map[string]string `toml:"checksum"`
	Images    map[string]string `toml:"image"`
	Store     map[string]string `toml:"store"`
	// Secrets lists what the user allowed each fetched package to read.
	Secrets map[string][]string `toml:"secrets"`
}

// SecretApprovalDepth is how far below the root NFB asks about secrets. Fetched packages deeper
// than this can't get a secret from the host, and fall back to their default.
const SecretApprovalDepth = 1

// SecretRequest is a secret a fetched package wants and hasn't been given.
type SecretRequest struct {
	Package  string
	Secret   string
	Optional bool
}

func (g *Graph) keepApprovals() {
	for _, u := range g.Units {
		if approved, ok := g.oldLock.Secrets[u.Name()]; ok && u.Remote {
			g.Lock.Secrets[u.Name()] = approved
		}
	}
}

// SecretAllowed reports whether a unit may read a secret from the host.
func (g *Graph) SecretAllowed(u *Unit, secret string) bool {
	if !u.Remote {
		return true
	}
	return u.Depth <= SecretApprovalDepth && slices.Contains(g.Lock.Secrets[u.Name()], secret)
}

// SecretRequests lists what still needs the user's answer.
func (g *Graph) SecretRequests() []SecretRequest {
	var requests []SecretRequest
	for _, u := range g.Units {
		if !u.Remote || u.Depth > SecretApprovalDepth {
			continue
		}
		for _, secret := range u.Manifest.Build.Secrets {
			if !g.SecretAllowed(u, secret) {
				_, optional := u.Manifest.Build.SecretDefaults[secret]
				requests = append(requests, SecretRequest{u.Name(), secret, optional})
			}
		}
	}
	return requests
}

func (g *Graph) ApproveSecret(req SecretRequest) {
	approved := append(slices.Clone(g.Lock.Secrets[req.Package]), req.Secret)
	slices.Sort(approved)
	g.Lock.Secrets[req.Package] = approved
}

func newLock() *Lock {
	return &Lock{
		Git:       map[string]string{},
		Checksums: map[string]string{},
		Images:    map[string]string{},
		Store:     map[string]string{},
		Secrets:   map[string][]string{},
	}
}

func ReadLock(path string) (*Lock, error) {
	lock := newLock()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return lock, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, lock); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return lock, nil
}

func (g *Graph) SaveLock() error {
	data, err := toml.Marshal(g.Lock)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(g.LockPath), ".nfb.lock-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err := errors.Join(err, tmp.Close(), os.Chmod(tmp.Name(), 0o644)); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), g.LockPath)
}

// LockChanged reports whether this run resolved anything differently from nfb.lock.
func (g *Graph) LockChanged() bool {
	return !reflect.DeepEqual(g.Lock, g.oldLock)
}

// PinImages fills Graph.Images with the digest form of each image.
func (g *Graph) PinImages(ctx context.Context) error {
	for _, u := range g.Units {
		image := u.Manifest.Build.Image
		if _, done := g.Images[image]; done {
			continue
		}
		digest := g.oldLock.Images[image]
		if strings.Contains(image, "@") {
			g.Images[image] = image
			continue
		}
		if digest == "" {
			digest = imageDigest(ctx, image)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if digest == "" {
			slog.Warn("could not find a digest, using the tag unpinned", "image", image)
			g.Images[image] = image
			continue
		}
		g.Lock.Images[image] = digest
		g.Images[image] = image + "@" + digest
	}
	return nil
}

func imageDigest(ctx context.Context, image string) string {
	ctx, cancel := context.WithTimeout(ctx, stallTimeout)
	defer cancel()
	out, err := Command(ctx, "docker", "buildx", "imagetools", "inspect", image, "--format", "{{.Manifest.Digest}}").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	out, err = Command(ctx, "docker", "image", "inspect", image, "--format", "{{index .RepoDigests 0}}").Output()
	if err != nil {
		return ""
	}
	if _, digest, found := strings.Cut(strings.TrimSpace(string(out)), "@"); found {
		return digest
	}
	return ""
}
