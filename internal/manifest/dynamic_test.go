package manifest

import (
	"strings"
	"testing"
)

const dynamicPackage = `[package]
name = "tool"
version = "2.0"

[source]
url = "https://example.com/{version}/tool-{version}.tar.gz"
sha256 = "` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"

[build]
image = "x"
command = ['echo {version} ${version}']

[[output.artifact]]
from = "tool-{version}/"
to = "."
`

func TestDynamicPackageUsesItsOwnVersionByDefault(t *testing.T) {
	m, err := Parse([]byte(dynamicPackage))
	if err != nil {
		t.Fatal(err)
	}
	if m.Source.URL != "https://example.com/2.0/tool-2.0.tar.gz" || m.Source.SHA256 == "" {
		t.Errorf("got %+v", m.Source)
	}
}

func TestDynamicPackageAtAnotherVersion(t *testing.T) {
	m, err := ParseVersion([]byte(dynamicPackage), "1.5")
	if err != nil {
		t.Fatal(err)
	}
	if m.Package.Version != "1.5" || m.Source.URL != "https://example.com/1.5/tool-1.5.tar.gz" {
		t.Errorf("got %s, %s", m.Package.Version, m.Source.URL)
	}
	if m.Source.SHA256 != "" {
		t.Error("the pinned checksum belongs to 2.0, it must not be used for 1.5")
	}
	if m.Build.Commands[0] != "echo 1.5 ${version}" {
		t.Errorf("a shell variable must be left alone: %s", m.Build.Commands[0])
	}
}

func TestFixedPackageIgnoresTheVersion(t *testing.T) {
	fixed := strings.ReplaceAll(strings.ReplaceAll(dynamicPackage, "${version}", ""), "{version}", "2.0")
	if Dynamic([]byte(fixed)) {
		t.Fatal("not dynamic")
	}
	m, err := ParseVersion([]byte(fixed), "1.5")
	if err != nil {
		t.Fatal(err)
	}
	if m.Package.Version != "2.0" {
		t.Errorf("got %s", m.Package.Version)
	}
}

func TestProjectsKeepVersionText(t *testing.T) {
	m, err := Parse([]byte("[project]\nname = \"p\"\n[build]\nimage = \"x\"\ncommand = ['echo {version}']\n[[output.artifact]]\nfrom = \"a\"\nto = \".\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Build.Commands[0] != "echo {version}" {
		t.Errorf("got %s", m.Build.Commands[0])
	}
}
