package cmd

import (
	"errors"
	"os"
	"strings"
)

const (
	tomlTypeProject = "project"
	tomlTypePackage = "package"
)

var unknownProjectTypeError = errors.New("unknown project type")

func initToml(projectName string, projectType string) error {
	replacer := strings.NewReplacer("{{name}}", projectName)

	switch projectType {
	case tomlTypeProject:
		return os.WriteFile("build.nfb.toml", []byte(replacer.Replace(defaultProjectTemplate)), 0644)
	case tomlTypePackage:
		return os.WriteFile("package.nfb.toml", []byte(replacer.Replace(defaultPackageTemplate)), 0644)
	default:
		return unknownProjectTypeError
	}
}

const defaultProjectTemplate = `
[project]
name = "{projectName}" # name in the project section cannot be removed and must be present.
version = "0.1.0"
output = "dist/{projectName}"
 
[build]
image = "debian:bookworm-slim" # can be any docker image
 
command = [
  "echo 'add your build steps here'"
]
 
# [[dependency]]
# name = "example" # optional, for debugging/readability only
# source = "cmd"    # cmd | git | download | package
# command = [
#   "apt install -y example"
# ]
 
[output]
dir = "dist" # the directory where the artifacts live
 
[[output.artifact]]
from = "bin/{projectName}"
to = "{projectName}"
`

const defaultPackageTemplate = `
[package]
name = "{projectName}" # name in the package definition must be present too.
version = "0.1.0"
 
[build] # exact same build step setup as a project manifest
image = "debian:bookworm-slim"
 
command = [
  "echo 'add your build steps here'"
]
 
# no need to define [output] dir, it is handled by NFB.
# [[output.artifact]]
# from = "bin"
# to = "." # will output it to {projectName}/bin/ since 'bin' has no trailing slash
 
# ran before the build step of a consuming project, in the same shell session
# [init]
# command = [
#   "export EXAMPLE_PATH=bin/"
# ]
`
