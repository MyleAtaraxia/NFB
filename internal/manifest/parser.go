package manifest

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Manifest struct {
	Project      *ProjectConfig `toml:"project"`
	Package      *PackageConfig `toml:"package"`
	Build        BuildConfig    `toml:"build"`
	Dependencies []Dependency   `toml:"dependency"`
	Output       *OutputConfig  `toml:"output"`
	Init         *InitConfig    `toml:"init"`
}

type ProjectConfig struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Output  string `toml:"output"`
}

type PackageConfig struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type BuildConfig struct {
	Image    string            `toml:"image"`
	Env      map[string]string `toml:"env"`
	Commands []string          `toml:"command"`
}

type Dependency struct {
	Name     string   `toml:"name"`
	Source   string   `toml:"source"`
	URL      string   `toml:"url"`
	Ref      string   `toml:"ref"`
	Path     string   `toml:"path"`
	Package  string   `toml:"package"`
	Commands []string `toml:"command"`
}

type OutputConfig struct {
	Dir       string           `toml:"dir"`
	Artifacts []OutputArtifact `toml:"artifact"`
}

type OutputArtifact struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

type InitConfig struct {
	Commands []string `toml:"command"`
}

func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
