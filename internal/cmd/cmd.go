package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	manifestPath string
	outputPath   string
	noCache      bool
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:   "nfb",
	Short: "Nix for Builds",
	Long:  `NFB (Nix for Builds) provides declarable and reproducible dependency management for build systems, just like NIX but anywhere and for builds (who would've thought)'.`,
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Resolve dependencies and build the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("resolving manifest: %s\n", manifestPath)
		// TODO: parse manifest -> resolve dependency graph -> generate Dockerfile -> docker build --output type=local
		return nil
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter manifest in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := promptString("What is the name of your project? (default is 'NfbProject'", "NfbProject")
		projectType, err := promptString("What is your project's type?\n(accepted values are 'project' and 'Package'. The default is project)", "project")
		if err != nil {
			return err
		}

		return initToml(name, projectType)
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove build artifacts and cached output",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: rm dist/ + clear cache dir
		return nil
	},
}

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Print the resolved dependency graph without building",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: parse manifest -> resolve -> print topo-sorted order
		return nil
	},
}

func init() {
	buildCmd.Flags().StringVarP(&manifestPath, "manifest", "m", "build.nfb.toml", "Path to the manifest file")
	buildCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Override the output path from the manifest")
	buildCmd.Flags().BoolVar(&noCache, "no-cache", false, "Force a clean rebuild, skip cache mounts")
	buildCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Stream full Docker build output")

	rootCmd.AddCommand(buildCmd, initCmd, cleanCmd, graphCmd)
}

func Execute() error {
	fmt.Println("\nWell hello there~")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	return rootCmd.Execute()
}
