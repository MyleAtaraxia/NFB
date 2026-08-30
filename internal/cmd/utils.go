package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func promptString(question string, defaultValue string) (string, error) {
	fmt.Print(question + ": ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input != "" {
		return input, nil
	}
	return defaultValue, nil
}

func resolveManifestPath(givenPath, defaultFilename string) string {
	if givenPath == "" {
		return defaultFilename
	}
	if filepath.Ext(givenPath) == ".toml" {
		return givenPath
	}
	return filepath.Join(givenPath, defaultFilename)
}
