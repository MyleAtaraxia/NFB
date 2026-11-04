package manifest

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	envPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	imagePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	storeName     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	storeVersion  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)
)

// StoreName reports whether a name can be used in the package store.
func StoreName(name string) bool { return storeName.MatchString(name) }

// StoreVersion reports whether a version can be used in the package store.
func StoreVersion(version string) bool { return storeVersion.MatchString(version) }

func Validate(m *Manifest) error {
	if m.Project == nil && m.Package == nil {
		return errors.New("manifest needs a [project] or [package] section")
	}
	if m.Project != nil && m.Package != nil {
		return errors.New("manifest cannot have both [project] and [package]")
	}
	if !namePattern.MatchString(m.Identity().Name) {
		return fmt.Errorf("invalid name %q", m.Identity().Name)
	}
	if !imagePattern.MatchString(m.Build.Image) {
		return fmt.Errorf("invalid or missing build image %q", m.Build.Image)
	}
	for key := range m.Build.Env {
		if !envPattern.MatchString(key) {
			return fmt.Errorf("invalid env name %q", key)
		}
	}
	for _, name := range m.Build.Secrets {
		if !envPattern.MatchString(name) {
			return fmt.Errorf("invalid secret name %q", name)
		}
	}
	for name := range m.Build.SecretDefaults {
		if !slices.Contains(m.Build.Secrets, name) {
			return fmt.Errorf("secret_defaults has %q, which isn't in secrets", name)
		}
	}

	if m.Project != nil {
		if err := validateProject(m); err != nil {
			return err
		}
	} else if err := validatePackage(m); err != nil {
		return err
	}

	for _, dep := range m.Dependencies {
		if err := validateDependency(dep); err != nil {
			return err
		}
	}
	return validateArtifacts(m.Artifacts())
}

func validateProject(m *Manifest) error {
	switch {
	case len(m.Build.Commands) == 0:
		return errors.New("project has no build commands")
	case m.Source != nil:
		return errors.New("[source] is only valid in a package")
	case m.Init != nil:
		return errors.New("[init] is only valid in a package, a project cannot be imported")
	case len(m.Artifacts()) == 0:
		return errors.New("project has no artifacts")
	}
	return nil
}

func validatePackage(m *Manifest) error {
	if m.Package.Version == "" {
		return errors.New("package has no version")
	}
	if m.Output != nil && m.Output.Dir != "" {
		return errors.New("[output] dir cannot be set in a package")
	}
	if m.Source != nil {
		if err := validateURL(m.Source.URL); err != nil {
			return fmt.Errorf("[source]: %w", err)
		}
		if ArchiveExt(m.Source.URL) == "" {
			return fmt.Errorf("[source]: %s is not a .tar.gz, .tar.xz or .zip", m.Source.URL)
		}
		if err := validateSHA256(m.Source.SHA256); err != nil {
			return fmt.Errorf("[source]: %w", err)
		}
	}
	return nil
}

func validateDependency(dep Dependency) error {
	label := dep.Name
	if label == "" {
		label = dep.Source + " dependency"
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", label, fmt.Sprintf(format, args...))
	}
	forbid := func(fields map[string]bool) error {
		for field, set := range fields {
			if set {
				return fail("field %q is not valid for source %q", field, dep.Source)
			}
		}
		return nil
	}

	url, ref, path, sha, command, version := dep.URL != "", dep.Ref != "", dep.Path != "", dep.SHA256 != "", len(dep.Commands) > 0, dep.Version != ""

	switch dep.Source {
	case "cmd":
		if !command {
			return fail("source \"cmd\" needs command")
		}
		return forbid(map[string]bool{"url": url, "ref": ref, "path": path, "sha256": sha, "version": version})
	case "git":
		if !url || !ref {
			return fail("source \"git\" needs url and ref")
		}
		if strings.HasPrefix(dep.Ref, "-") {
			return fail("invalid ref %q", dep.Ref)
		}
		if path && !filepath.IsLocal(filepath.FromSlash(dep.Path)) {
			return fail("git path %q must stay inside the repository", dep.Path)
		}
		return forbid(map[string]bool{"sha256": sha, "command": command, "version": version})
	case "url":
		if err := validateURL(dep.URL); err != nil {
			return fail("%v", err)
		}
		if err := validateSHA256(dep.SHA256); err != nil {
			return fail("%v", err)
		}
		return forbid(map[string]bool{"ref": ref, "path": path, "command": command, "version": version})
	case "local":
		if !path {
			return fail("source \"local\" needs path")
		}
		return forbid(map[string]bool{"url": url, "ref": ref, "sha256": sha, "command": command, "version": version})
	case "store":
		if !StoreName(dep.Name) {
			return fail("source \"store\" needs the package's name")
		}
		if !StoreVersion(dep.Version) {
			return fail("source \"store\" needs an exact version")
		}
		return forbid(map[string]bool{"url": url, "ref": ref, "path": path, "sha256": sha, "command": command})
	default:
		return fail("unknown source %q", dep.Source)
	}
}

func validateArtifacts(artifacts []Artifact) error {
	seen := map[string]bool{}
	for _, a := range artifacts {
		if a.From == "" || a.To == "" {
			return errors.New("artifact needs from and to")
		}
		for _, p := range []string{a.From, a.To} {
			if strings.ContainsAny(p, "*?[$\\\n") {
				return fmt.Errorf("artifact path %q has an unsupported character", p)
			}
		}
		if !filepath.IsLocal(filepath.FromSlash(a.To)) {
			return fmt.Errorf("artifact to %q must stay inside the output folder", a.To)
		}
		// shortcut: only catches identical destinations, overlaps inside folders need the built file list.
		if dest := a.Dest(); seen[dest] {
			return fmt.Errorf("two artifacts write to %s", dest)
		} else {
			seen[dest] = true
		}
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) url", raw)
	}
	return nil
}

func validateSHA256(sum string) error {
	if sum != "" && !sha256Pattern.MatchString(sum) {
		return fmt.Errorf("%q is not a sha256 checksum", sum)
	}
	return nil
}

// ArchiveExt returns the archive type of a download URL, or "" if unsupported.
func ArchiveExt(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, ext := range []string{".tar.gz", ".tar.xz", ".zip"} {
		if strings.HasSuffix(u.Path, ext) {
			return ext
		}
	}
	return ""
}
