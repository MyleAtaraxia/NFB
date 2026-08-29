package cmd

import "errors"

const (
	tomlTypeProject = "project"
	tomlTypeCommand = "package"
)

var unknownProjectTypeError = errors.New("unknown project type")

func initToml(projectName string, projectType string) error {
	//TODO: Initialize correct TOMLs
	switch projectType {
	case tomlTypeProject:

	case tomlTypeCommand:

	}

	return unknownProjectTypeError
}
