package manifest

import (
	"errors"
	"log/slog"
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

var UnspecifiedManifestImage = errors.New("unspecified manifest image")
var unknownManifestTypeError = errors.New("unknown manifest type")
var doubleDefinedManifestTypeError = errors.New("double defined manifest type")
var manifestNameUndefinedError = errors.New("manifest name undefined")
var manifestIncorrectSourceError = errors.New("manifest incorrect source")
var manifestIncorrectArtifactError = errors.New("manifest incorrect artifact")
var noBuildCommandsError = errors.New("no build commands provided")

func ValidateManifest(manifest *Manifest) error {
	if manifest.Project == nil && manifest.Package == nil {
		return unknownManifestTypeError
	}
	if manifest.Project != nil && manifest.Package != nil {
		return doubleDefinedManifestTypeError
	}
	if manifest.Project != nil {
		if manifest.Project.Name == "" {
			return manifestNameUndefinedError
		}
	}
	if manifest.Package != nil {
		if manifest.Package.Name == "" {
			return manifestNameUndefinedError
		}
	}
	for _, dependency := range manifest.Dependencies {
		depID := dependency.Name
		if depID == "" {
			depID = dependency.URL
		}
		switch dependency.Source {
		case "git":
			if dependency.URL == "" || dependency.Ref == "" {
				slog.Error("manifest validation failed", "dependency", depID, "error", manifestIncorrectSourceError)
				return manifestIncorrectSourceError
			}
		case "cmd":
			if len(dependency.Commands) == 0 {
				slog.Error("manifest validation failed", "dependency", depID, "error", manifestIncorrectSourceError)
				return manifestIncorrectSourceError
			}
		case "package":
			if dependency.Path == "" && dependency.URL == "" {
				slog.Error("manifest validation failed", "dependency", depID, "error", manifestIncorrectSourceError)
				return manifestIncorrectSourceError
			}
		default:
			slog.Error("manifest validation failed", "dependency", depID, "error", manifestIncorrectSourceError)
			return manifestIncorrectSourceError
		}
	}
	if manifest.Output != nil {
		for _, artifact := range manifest.Output.Artifacts {
			if artifact.From == "" {
				slog.Error("manifest validation failed", "artifact_to", artifact.To, "error", manifestIncorrectArtifactError)
				return manifestIncorrectArtifactError
			}
			if artifact.To == "" {
				slog.Error("manifest validation failed", "artifact_from", artifact.From, "error", manifestIncorrectArtifactError)
				return manifestIncorrectArtifactError
			}
		}
	}
	if len(manifest.Build.Commands) == 0 {
		return noBuildCommandsError
	}
	if manifest.Build.Image == "" {
		return UnspecifiedManifestImage
	}

	return nil
}
