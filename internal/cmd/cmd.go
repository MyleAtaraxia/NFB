package cmd

import (
	"NFB/internal/builder"
	"NFB/internal/resolver"
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	manifestPath string
	outputPath   string
	noCache      bool
	verbose      bool
	initPath     string
)

var rootCmd = &cobra.Command{
	Use:          "nfb",
	Short:        "Nix for Builds",
	Long:         `NFB (Nix for Builds) provides declarable and reproducible dependency management for build systems, just like NIX but anywhere and for builds (who would've thought)'.`,
	SilenceUsage: true,
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Resolve dependencies and build the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := resolver.Resolve(manifestPath, noCache)
		if err != nil {
			return err
		}
		g.PinImages()
		if err := g.SaveLock(); err != nil {
			return err
		}
		out, err := builder.Build(g, builder.Options{OutputOverride: outputPath, NoCache: noCache, Verbose: verbose})
		if err != nil {
			return err
		}
		fmt.Println("built", g.Root().Name(), "to", out)
		return nil
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter manifest in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)
		name, err := prompt(reader, "What is the name of your project? (default is 'NfbProject')", "NfbProject")
		if err != nil {
			return err
		}
		projectType, err := prompt(reader, "What is your project's type? (accepted values are 'project' and 'package', the default is project)", "project")
		if err != nil {
			return err
		}
		return initToml(name, strings.ToLower(projectType), initPath)
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove NFB's cache of fetched repositories and downloads",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolver.CacheDir()
		if err != nil {
			return err
		}
		return os.RemoveAll(dir)
	},
}

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Print the resolved dependency graph in build order",
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := resolver.Resolve(manifestPath, false)
		if err != nil {
			return err
		}
		g.PinImages()
		if err := g.SaveLock(); err != nil {
			return err
		}
		for _, u := range g.Units {
			var deps []string
			for _, dep := range u.Deps {
				deps = append(deps, dep.Name())
			}
			fmt.Printf("%s %s -> [%s]\n", u.Name(), u.Manifest.Identity().Version, strings.Join(deps, ", "))
		}
		return nil
	},
}

func prompt(reader *bufio.Reader, question, defaultValue string) (string, error) {
	fmt.Print(question + ": ")
	input, err := reader.ReadString('\n')
	if err != nil && input == "" {
		return defaultValue, nil
	}
	if input = strings.TrimSpace(input); input != "" {
		return input, nil
	}
	return defaultValue, nil
}

func init() {
	buildCmd.Flags().StringVarP(&manifestPath, "manifest", "m", "build.nfb.toml", "Path to the manifest file")
	buildCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Override the output path from the manifest")
	buildCmd.Flags().BoolVar(&noCache, "no-cache", false, "Force a clean rebuild and refetch everything")
	buildCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Stream full Docker build output")
	graphCmd.Flags().StringVarP(&manifestPath, "manifest", "m", "build.nfb.toml", "Path to the manifest file")
	initCmd.Flags().StringVarP(&initPath, "path", "p", "", "Directory or full path (including filename) to write the manifest to. Defaults to the current directory using the standard filename for the chosen type.")

	rootCmd.AddCommand(buildCmd, initCmd, cleanCmd, graphCmd)
}

func Execute() error {
	fmt.Println("\nWell hello there~")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	return rootCmd.Execute()
}
