package manifest

import (
	"strings"
	"testing"
)

const validPackage = `
[package]
name = "pkg"
version = "1.0.0"
[build]
image = "debian:bookworm-slim"
command = ["true"]
`

const validProject = `
[project]
name = "app"
[build]
image = "debian:bookworm-slim"
command = ["true"]
[[output.artifact]]
from = "a"
to = "b"
`

func TestDocumentedMatrix(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"a/tool", "bin/app", "/bin/app"},
		{"a/tool", "bin/", "/bin/tool"},
		{"a/tool", ".", "/tool"},
		{"repo/bin", ".", "/bin"},
		{"repo/bin", "libs/", "/libs/bin"},
		{"repo/bin", "libs", "/libs"},
		{"repo/bin/", ".", "/"},
		{"repo/bin/", "libs/", "/libs/"},
		{"repo/bin/", "libs", "/libs/"},
	}
	for _, c := range cases {
		if got := (Artifact{From: c.from, To: c.to}).Dest(); got != c.want {
			t.Errorf("%s -> %s: got %s, want %s", c.from, c.to, got, c.want)
		}
	}
}

func TestAcceptsValidManifests(t *testing.T) {
	for _, src := range []string{validPackage, validProject} {
		if _, err := Parse([]byte(src)); err != nil {
			t.Error(err)
		}
	}
}

func TestRejects(t *testing.T) {
	sum := strings.Repeat("a", 64)
	dep := func(body string) string { return validPackage + "[[dependency]]\n" + body }
	cases := map[string]string{
		"no type":               `[build]` + "\nimage=\"x\"\ncommand=[\"y\"]",
		"both types":            validPackage + "[project]\nname = \"a\"\n",
		"unknown field":         validPackage + "typo = 1\n",
		"bad name":              strings.Replace(validPackage, `"pkg"`, `"../x"`, 1),
		"package no version":    strings.Replace(validPackage, `version = "1.0.0"`, "", 1),
		"bad image":             strings.Replace(validPackage, "debian:bookworm-slim", "a\\nRUN evil", 1),
		"bad secret name":       strings.Replace(validPackage, `command = ["true"]`, `secrets = ["A B"]`, 1),
		"git ref is a flag":     dep("source = \"git\"\nurl = \"u\"\nref = \"--upload-pack=x\"\n"),
		"project init":          validProject + "[init]\ncommand = [\"x\"]\n",
		"project source":        validProject + "[source]\nurl = \"https://a.b/c.zip\"\n",
		"project no artifacts":  strings.Split(validProject, "[[output")[0],
		"package output dir":    validPackage + "[output]\ndir = \"x\"\n",
		"source bad archive":    validPackage + "[source]\nurl = \"https://a.b/c.rar\"\n",
		"source bad sha":        validPackage + "[source]\nurl = \"https://a.b/c.zip\"\nsha256 = \"...\"\n",
		"cmd needs command":     dep("source = \"cmd\"\n"),
		"cmd extra field":       dep("source = \"cmd\"\ncommand = [\"x\"]\nurl = \"u\"\n"),
		"git needs ref":         dep("source = \"git\"\nurl = \"u\"\n"),
		"git dotdot path":       dep("source = \"git\"\nurl = \"u\"\nref = \"r\"\npath = \"../x\"\n"),
		"git absolute path":     dep("source = \"git\"\nurl = \"u\"\nref = \"r\"\npath = \"/x\"\n"),
		"git extra field":       dep("source = \"git\"\nurl = \"u\"\nref = \"r\"\nsha256 = \"" + sum + "\"\n"),
		"url scheme":            dep("source = \"url\"\nurl = \"file:///etc/passwd\"\n"),
		"url extra field":       dep("source = \"url\"\nurl = \"https://a.b/c\"\nref = \"r\"\n"),
		"local needs path":      dep("source = \"local\"\n"),
		"local extra field":     dep("source = \"local\"\npath = \"x\"\nurl = \"u\"\n"),
		"unknown source":        dep("source = \"package\"\n"),
		"artifact escapes":      validPackage + "[[output.artifact]]\nfrom = \"a\"\nto = \"../b\"\n",
		"artifact absolute":     validPackage + "[[output.artifact]]\nfrom = \"a\"\nto = \"/b\"\n",
		"artifact wildcard":     validPackage + "[[output.artifact]]\nfrom = \"*.dll\"\nto = \"b\"\n",
		"artifact same dest":    validPackage + "[[output.artifact]]\nfrom = \"a\"\nto = \"b\"\n[[output.artifact]]\nfrom = \"c\"\nto = \"b\"\n",
		"artifact missing from": validPackage + "[[output.artifact]]\nto = \"b\"\n",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAllowsLocalDotDotAndOptionalSHA(t *testing.T) {
	src := validPackage + `
[[dependency]]
source = "local"
path = "../../libs/foo/package.nfb.toml"

[[dependency]]
source = "url"
url = "https://a.b/c.toml"
`
	if _, err := Parse([]byte(src)); err != nil {
		t.Error(err)
	}
}
