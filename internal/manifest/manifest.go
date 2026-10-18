package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Manifest struct {
	Project      *Identity    `toml:"project"`
	Package      *Identity    `toml:"package"`
	Source       *Source      `toml:"source"`
	Build        Build        `toml:"build"`
	Dependencies []Dependency `toml:"dependency"`
	Output       *Output      `toml:"output"`
	Init         *Init        `toml:"init"`
}

type Identity struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type Source struct {
	URL    string `toml:"url"`
	SHA256 string `toml:"sha256"`
}

type Build struct {
	Image    string            `toml:"image"`
	Env      map[string]string `toml:"env"`
	Commands []string          `toml:"command"`
	Secrets  []string          `toml:"secrets"`
}

type Dependency struct {
	Name     string   `toml:"name"`
	Source   string   `toml:"source"`
	URL      string   `toml:"url"`
	Ref      string   `toml:"ref"`
	Path     string   `toml:"path"`
	SHA256   string   `toml:"sha256"`
	Commands []string `toml:"command"`
}

type Output struct {
	Dir       string     `toml:"dir"`
	Artifacts []Artifact `toml:"artifact"`
}

type Artifact struct {
	From string `toml:"from"`
	To   string `toml:"to"`
}

type Init struct {
	Commands []string `toml:"command"`
}

func (m *Manifest) Identity() *Identity {
	if m.Package != nil {
		return m.Package
	}
	return m.Project
}

func (m *Manifest) Artifacts() []Artifact {
	if m.Output == nil {
		return nil
	}
	return m.Output.Artifacts
}

// Dest is the artifact's absolute path inside the output root.
func (a Artifact) Dest() string {
	switch {
	case strings.HasSuffix(a.From, "/"):
		return strings.TrimRight(path.Join("/", a.To), "/") + "/"
	case a.To == "." || strings.HasSuffix(a.To, "/"):
		return path.Join("/", a.To, path.Base(a.From))
	default:
		return path.Join("/", a.To)
	}
}

func Load(file string) (*Manifest, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return m, nil
}

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&m); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("unknown field:\n%s", strict.String())
		}
		return nil, err
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
