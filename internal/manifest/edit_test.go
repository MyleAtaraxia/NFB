package manifest

import (
	"strings"
	"testing"
)

const editBase = `# my project
[project]
name = "app"

[build]
image = "x" # keep this comment
command = ["make"]

[[dependency]]
name = "gcc"
source = "cmd"
command = [
  "apt install gcc"
]

# what we ship
[[output.artifact]]
from = "a"
to = "."
`

func TestAddKeepsCommentsAndLayout(t *testing.T) {
	out, err := AddStoreDependency([]byte(editBase), "llvm", "19.1.0")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"# my project", "# keep this comment", "# what we ship"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "]\n\n[[dependency]]\nname = \"llvm\"\nsource = \"store\"\nversion = \"19.1.0\"\n\n# what we ship") {
		t.Errorf("unexpected layout:\n%s", got)
	}
}

func TestAddAtEndOfFileAndChangeVersion(t *testing.T) {
	base := "[project]\nname = \"app\"\n[build]\nimage = \"x\"\ncommand = [\"y\"]\n[[output.artifact]]\nfrom = \"a\"\nto = \".\"\n"
	out, err := AddStoreDependency([]byte(base), "llvm", "1")
	if err != nil {
		t.Fatal(err)
	}
	if out, err = AddStoreDependency(out, "llvm", "2"); err != nil {
		t.Fatal(err)
	}
	m, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Dependencies) != 1 || m.Dependencies[0].Version != "2" {
		t.Errorf("dependencies: %+v", m.Dependencies)
	}
}

func TestEditsKeepWindowsLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(editBase, "\n", "\r\n")
	out, err := AddStoreDependency([]byte(crlf), "llvm", "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), "\n") != strings.Count(string(out), "\r\n") {
		t.Errorf("mixed line endings:\n%q", out)
	}
}

func TestRemoveRestoresTheManifest(t *testing.T) {
	out, err := AddStoreDependency([]byte(editBase), "llvm", "1")
	if err != nil {
		t.Fatal(err)
	}
	if out, err = RemoveStoreDependency(out, "llvm"); err != nil {
		t.Fatal(err)
	}
	if string(out) != editBase {
		t.Errorf("got:\n%s", out)
	}
	if _, err := RemoveStoreDependency([]byte(editBase), "gcc"); err == nil {
		t.Error("a cmd dependency isn't a store package")
	}
}

func TestPinSource(t *testing.T) {
	sum := strings.Repeat("a", 64)
	base := "[package]\nname = \"p\"\nversion = \"1\"\n\n[source]\nurl = \"https://x.dev/a.tar.gz\"\n\n[build]\nimage = \"x\"\n"
	out, err := PinSource([]byte(base), sum)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "url = \"https://x.dev/a.tar.gz\"\nsha256 = \""+sum+"\"\n") {
		t.Errorf("got:\n%s", out)
	}
	other := strings.Repeat("b", 64)
	if out, err = PinSource(out, other); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), sum) || !strings.Contains(string(out), other) {
		t.Errorf("didn't replace the checksum:\n%s", out)
	}
}

func TestStoreDependencyAndSecretValidation(t *testing.T) {
	head := "[project]\nname = \"app\"\n[build]\nimage = \"x\"\ncommand = [\"y\"]\n"
	tail := "[[output.artifact]]\nfrom = \"a\"\nto = \".\"\n"
	cases := map[string]string{
		"no version":   "[[dependency]]\nsource = \"store\"\nname = \"llvm\"\n",
		"no name":      "[[dependency]]\nsource = \"store\"\nversion = \"1\"\n",
		"capitals":     "[[dependency]]\nsource = \"store\"\nname = \"LLVM\"\nversion = \"1\"\n",
		"range":        "[[dependency]]\nsource = \"store\"\nname = \"llvm\"\nversion = \"^1\"\n",
		"extra field":  "[[dependency]]\nsource = \"store\"\nname = \"llvm\"\nversion = \"1\"\nurl = \"https://x.dev\"\n",
		"version else": "[[dependency]]\nsource = \"cmd\"\nversion = \"1\"\ncommand = [\"y\"]\n",
	}
	for name, dep := range cases {
		if _, err := Parse([]byte(head + dep + tail)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	stray := strings.Replace(head, "command = [\"y\"]\n", "command = [\"y\"]\nsecret_defaults = { A = \"\" }\n", 1)
	if _, err := Parse([]byte(stray + tail)); err == nil {
		t.Error("a default for an undeclared secret should be an error")
	}
	good := strings.Replace(head, "command = [\"y\"]\n", "command = [\"y\"]\nsecrets = [\"A\"]\nsecret_defaults = { A = \"\" }\n", 1)
	if _, err := Parse([]byte(good + tail)); err != nil {
		t.Error(err)
	}
}
