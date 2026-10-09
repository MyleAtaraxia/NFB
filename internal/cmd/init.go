package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func initToml(name, projectType, pathOverride string) error {
	var template, filename string
	switch projectType {
	case "project":
		template, filename = defaultProjectTemplate, "build.nfb.toml"
	case "package":
		template, filename = defaultPackageTemplate, "package.nfb.toml"
	default:
		return fmt.Errorf("unknown project type %q", projectType)
	}

	path := pathOverride
	if path == "" {
		path = filename
	} else if filepath.Ext(path) != ".toml" {
		path = filepath.Join(path, filename)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(strings.ReplaceAll(template, "{{projectName}}", name))
	return err
}

const defaultProjectTemplate = `[project]
name = "{{projectName}}" # name in the project section cannot be removed and must be present.
version = "0.1.0"

[build]
image = "debian:bookworm-slim" # can be any docker image
command = [
  "mkdir -p bin",
  "echo 'add your build steps here' > bin/{{projectName}}"
]

# [[dependency]]
# name = "example" # optional, for debugging/readability only
# source = "cmd"   # cmd | git | url | local
# command = [
#   "apt install -y example"
# ]

[output]
dir = "dist" # the directory where the artifacts live

[[output.artifact]]
from = "bin/{{projectName}}"
to = "{{projectName}}"
`

const defaultPackageTemplate = `[package]
name = "{{projectName}}" # name in the package definition must stay the same.
version = "0.1.0"

[build] # exact same build step setup as a project manifest
image = "debian:bookworm-slim"
command = [
  "mkdir -p bin",
  "echo 'add your build steps here' > bin/{{projectName}}"
]

# no need to define [output] dir, it is handled by NFB.
[[output.artifact]]
from = "bin"
to = "." # will output it to {{projectName}}/bin/ as bin has no trailing slash

# ran when the package is imported, from the package's root output folder
[init]
command = [
  'export PATH="$PWD/bin:$PATH"'
]
`
