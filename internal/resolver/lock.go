package resolver

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Lock pins what a manifest leaves floating.
type Lock struct {
	Git       map[string]string `toml:"git"`
	Checksums map[string]string `toml:"checksum"`
	Images    map[string]string `toml:"image"`
}

func newLock() *Lock {
	return &Lock{Git: map[string]string{}, Checksums: map[string]string{}, Images: map[string]string{}}
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
	return os.WriteFile(g.LockPath, data, 0o644)
}

// PinImages fills Graph.Images with the digest form of each image.
func (g *Graph) PinImages() {
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
		if digest == "" || g.noCache {
			digest = imageDigest(image)
		}
		if digest == "" {
			slog.Warn("could not find a digest, using the tag unpinned", "image", image)
			g.Images[image] = image
			continue
		}
		g.Lock.Images[image] = digest
		g.Images[image] = image + "@" + digest
	}
}

func imageDigest(image string) string {
	out, err := exec.Command("docker", "buildx", "imagetools", "inspect", image, "--format", "{{.Manifest.Digest}}").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	out, err = exec.Command("docker", "image", "inspect", image, "--format", "{{index .RepoDigests 0}}").Output()
	if err != nil {
		return ""
	}
	if _, digest, found := strings.Cut(strings.TrimSpace(string(out)), "@"); found {
		return digest
	}
	return ""
}
