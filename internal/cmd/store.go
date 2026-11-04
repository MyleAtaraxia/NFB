package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MyleAtaraxia/nfb/internal/builder"
	"github.com/MyleAtaraxia/nfb/internal/manifest"
	"github.com/MyleAtaraxia/nfb/internal/resolver"
	"github.com/MyleAtaraxia/nfb/internal/store"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

var (
	storeRepo    string
	storeBase    string
	storeRawBase string
	storeOutput  string
	storeBuild   bool
	pinPath      string
)

// approveSecrets asks about secrets fetched packages want. With onlyRequired, a secret that has
// a default stays unapproved without asking, so saying no doesn't mean a prompt on every build.
func approveSecrets(g *resolver.Graph, onlyRequired bool) error {
	reader := bufio.NewReader(os.Stdin)
	for _, req := range g.SecretRequests() {
		if onlyRequired && req.Optional {
			continue
		}
		kind := ""
		if req.Optional {
			kind = "optional "
		}
		answer, err := prompt(reader, fmt.Sprintf("%s asks for the %ssecret %s. Allow it? (y/N)", req.Package, kind, req.Secret), "n")
		if err != nil {
			return err
		}
		if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
			g.ApproveSecret(req)
		}
	}
	return nil
}

// applyManifest writes an edited manifest and rolls it back if the project no longer resolves.
func applyManifest(ctx context.Context, original, edited []byte, askSecrets bool) error {
	if err := os.WriteFile(manifestPath, edited, 0o644); err != nil {
		return err
	}
	err := func() error {
		g, err := resolver.Resolve(ctx, manifestPath, resolver.Options{})
		if err != nil {
			return err
		}
		if err := g.PinImages(ctx); err != nil {
			return err
		}
		if askSecrets {
			if err := approveSecrets(g, false); err != nil {
				return err
			}
		}
		return g.SaveLock()
	}()
	if err != nil {
		return errors.Join(err, os.WriteFile(manifestPath, original, 0o644))
	}
	return nil
}

var installCmd = &cobra.Command{
	Use:   "install <package>[@version]",
	Short: "Add a package from the store to the manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, version, _ := strings.Cut(args[0], "@")
		s, err := resolver.FetchStore(cmd.Context())
		if err != nil {
			return err
		}
		if version, err = s.Lookup(name, version); err != nil {
			return err
		}
		host := builder.Platform(cmd.Context())
		if platforms := s.Packages[name].Platforms; host != "" && !slices.Contains(platforms, host) {
			fmt.Fprintf(os.Stderr, "warning: %s supports %s, and this machine builds for %s\n", name, strings.Join(platforms, ", "), host)
		}

		original, err := os.ReadFile(manifestPath)
		if err != nil {
			return err
		}
		edited, err := manifest.AddStoreDependency(original, name, version)
		if err != nil {
			return fmt.Errorf("%s: %w", manifestPath, err)
		}
		if err := applyManifest(cmd.Context(), original, edited, true); err != nil {
			return err
		}
		fmt.Printf("%s %s is all yours~\n", name, version)
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "Remove a store package from the manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		original, err := os.ReadFile(manifestPath)
		if err != nil {
			return err
		}
		edited, err := manifest.RemoveStoreDependency(original, args[0])
		if err != nil {
			return fmt.Errorf("%s: %w", manifestPath, err)
		}
		if err := applyManifest(cmd.Context(), original, edited, false); err != nil {
			return err
		}
		fmt.Printf("Bye bye, %s~\n", args[0])
		return nil
	},
}

var searchCmd = &cobra.Command{
	Use:   "search [term]",
	Short: "Search the package store",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := resolver.FetchStore(cmd.Context())
		if err != nil {
			return err
		}
		term := ""
		if len(args) > 0 {
			term = strings.ToLower(args[0])
		}
		found := false
		for _, name := range slices.Sorted(maps.Keys(s.Packages)) {
			p := s.Packages[name]
			if strings.Contains(name, term) || strings.Contains(strings.ToLower(p.Description), term) {
				fmt.Printf("%s %s - %s\n", name, p.Latest, p.Description)
				found = true
			}
		}
		if !found {
			fmt.Println("Nothing here, sorry~")
		}
		return nil
	},
}

var pinCmd = &cobra.Command{
	Use:   "pin",
	Short: "Download a package's [source] and write its sha256 into the manifest",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(pinPath)
		if err != nil {
			return err
		}
		m, err := manifest.Parse(data)
		if err != nil {
			return fmt.Errorf("%s: %w", pinPath, err)
		}
		if m.Source == nil {
			return fmt.Errorf("%s has no [source] section", pinPath)
		}
		sum, err := resolver.Checksum(cmd.Context(), m.Source.URL)
		if err != nil {
			return err
		}
		edited, err := manifest.PinSource(data, sum)
		if err != nil {
			return fmt.Errorf("%s: %w", pinPath, err)
		}
		if err := os.WriteFile(pinPath, edited, 0o644); err != nil {
			return err
		}
		fmt.Println("pinned", m.Source.URL, "to", sum)
		return nil
	},
}

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Tools for maintaining the package store repository",
}

var storeIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Generate metadata.json from the store's git history",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Index(cmd.Context(), storeRepo, "HEAD", storeRawBase)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(storeOutput), 0o755); err != nil {
			return err
		}
		return os.WriteFile(storeOutput, append(data, '\n'), 0o644)
	},
}

var storeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check the packages changed since a base commit",
	RunE: func(cmd *cobra.Command, args []string) error {
		names, err := store.Check(cmd.Context(), storeRepo, storeBase)
		if err != nil {
			return err
		}
		fmt.Println("checked", len(names), "packages")
		if !storeBuild {
			return nil
		}
		if err := builder.CheckDocker(cmd.Context()); err != nil {
			return err
		}
		for _, name := range names {
			if err := buildPackage(cmd.Context(), name); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	},
}

func buildPackage(ctx context.Context, name string) error {
	g, err := resolver.Resolve(ctx, filepath.Join(storeRepo, "packages", name, "package.nfb.toml"), resolver.Options{})
	if err != nil {
		return err
	}
	if err := g.PinImages(ctx); err != nil {
		return err
	}
	build := g.Root().Manifest.Build
	for _, secret := range build.Secrets {
		_, hasDefault := build.SecretDefaults[secret]
		if _, set := os.LookupEnv(secret); !set && !hasDefault {
			fmt.Printf("skipping the test build of %s, it needs the secret %s\n", name, secret)
			return nil
		}
	}
	out, err := os.MkdirTemp("", "nfb-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(out)
	_, err = builder.Build(ctx, g, builder.Options{OutputOverride: out})
	return err
}

func init() {
	for _, c := range []*cobra.Command{installCmd, removeCmd} {
		c.Flags().StringVarP(&manifestPath, "manifest", "m", "build.nfb.toml", "Path to the manifest file")
	}
	pinCmd.Flags().StringVarP(&pinPath, "manifest", "m", "package.nfb.toml", "Path to the package manifest")

	for _, c := range []*cobra.Command{storeIndexCmd, storeCheckCmd} {
		c.Flags().StringVar(&storeRepo, "repo", ".", "Path to the store repository")
	}
	storeIndexCmd.Flags().StringVar(&storeRawBase, "raw-base", "https://raw.githubusercontent.com/MyleAtaraxia/nfb-packages", "Where the repository's files are served, followed by /{commit}/{path}")
	storeIndexCmd.Flags().StringVarP(&storeOutput, "output", "o", "metadata.json", "Where to write the metadata")
	storeCheckCmd.Flags().StringVar(&storeBase, "base", "origin/main", "The commit to compare with")
	storeCheckCmd.Flags().BoolVar(&storeBuild, "build", false, "Also build the changed packages")
	storeCmd.AddCommand(storeIndexCmd, storeCheckCmd)

	rootCmd.AddCommand(installCmd, removeCmd, searchCmd, pinCmd, storeCmd)
}
