package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var (
	headerPattern  = regexp.MustCompile(`^\s*\[\[?\s*[A-Za-z]`)
	versionLine    = regexp.MustCompile(`^\s*version\s*=`)
	urlLine        = regexp.MustCompile(`^\s*url\s*=`)
	sha256Line     = regexp.MustCompile(`^\s*sha256\s*=`)
	commentOrBlank = regexp.MustCompile(`^\s*(#.*)?$`)
)

// These edits work on the text so comments and layout survive, and each result is parsed and
// compared to what was meant, so an unusual manifest fails instead of being damaged.

// AddStoreDependency adds a store package, or changes its version if it's already there.
func AddStoreDependency(data []byte, name, version string) ([]byte, error) {
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	want := slices.Clone(m.Dependencies)
	lines, cr := splitLines(data)

	if at := storeBlock(lines, name); at != nil {
		i := slices.IndexFunc(lines[at.start:at.end], versionLine.MatchString)
		if i < 0 {
			return nil, fmt.Errorf("couldn't find the version of %s", name)
		}
		lines[at.start+i] = fmt.Sprintf("version = %q%s", version, cr)
		for i := range want {
			if want[i].Source == "store" && want[i].Name == name {
				want[i].Version = version
			}
		}
		return checked(lines, want)
	}

	at := len(lines)
	if blocks := dependencyBlocks(lines); len(blocks) > 0 {
		at = blocks[len(blocks)-1].end
	}
	head := slices.Clone(lines[:at])
	for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
		head = head[:len(head)-1]
	}
	tail := lines[at:]
	for len(tail) > 0 && strings.TrimSpace(tail[0]) == "" {
		tail = tail[1:]
	}

	out := head
	if len(out) > 0 {
		out = append(out, cr)
	}
	out = append(out,
		"[[dependency]]"+cr,
		fmt.Sprintf("name = %q%s", name, cr),
		`source = "store"`+cr,
		fmt.Sprintf("version = %q%s", version, cr))
	if len(tail) > 0 {
		out = append(append(out, cr), tail...)
	} else {
		out = append(out, "")
	}
	return checked(out, append(want, Dependency{Name: name, Source: "store", Version: version}))
}

// RemoveStoreDependency removes a store package added by AddStoreDependency or by hand.
func RemoveStoreDependency(data []byte, name string) ([]byte, error) {
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	lines, _ := splitLines(data)
	at := storeBlock(lines, name)
	if at == nil {
		return nil, fmt.Errorf("%s isn't a store dependency of this manifest", name)
	}
	start := at.start
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	want := slices.DeleteFunc(slices.Clone(m.Dependencies), func(dep Dependency) bool {
		return dep.Source == "store" && dep.Name == name
	})
	return checked(slices.Concat(lines[:start], lines[at.end:]), want)
}

// PinSource writes the checksum of [source] into the manifest.
func PinSource(data []byte, sum string) ([]byte, error) {
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if m.Source == nil {
		return nil, errors.New("manifest has no [source] section")
	}
	lines, cr := splitLines(data)
	start := slices.IndexFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "[source]" })
	if start < 0 {
		return nil, errors.New("couldn't find the [source] section")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if headerPattern.MatchString(lines[i]) {
			end = i
			break
		}
	}

	section := lines[start:end]
	entry := fmt.Sprintf("sha256 = %q%s", sum, cr)
	if i := slices.IndexFunc(section, sha256Line.MatchString); i >= 0 {
		section[i] = entry
	} else if i := slices.IndexFunc(section, urlLine.MatchString); i >= 0 {
		lines = slices.Insert(lines, start+i+1, entry)
	} else {
		return nil, errors.New("couldn't find the url of [source]")
	}

	out := []byte(strings.Join(lines, "\n"))
	after, err := Parse(out)
	if err != nil {
		return nil, err
	}
	if after.Source.SHA256 != sum || after.Source.URL != m.Source.URL {
		return nil, errors.New("couldn't update [source] safely, set sha256 by hand")
	}
	return out, nil
}

func splitLines(data []byte) ([]string, string) {
	cr := ""
	if bytes.Contains(data, []byte("\r\n")) {
		cr = "\r"
	}
	return strings.Split(string(data), "\n"), cr
}

type block struct{ start, end int }

func dependencyBlocks(lines []string) []block {
	var blocks []block
	start := -1
	closeBlock := func(end int) {
		if start < 0 {
			return
		}
		for end > start+1 && commentOrBlank.MatchString(lines[end-1]) {
			end--
		}
		blocks = append(blocks, block{start, end})
		start = -1
	}
	for i, line := range lines {
		if headerPattern.MatchString(line) {
			closeBlock(i)
			if strings.TrimSpace(line) == "[[dependency]]" {
				start = i
			}
		}
	}
	closeBlock(len(lines))
	return blocks
}

func storeBlock(lines []string, name string) *block {
	for _, b := range dependencyBlocks(lines) {
		var parsed struct {
			Dependency []Dependency `toml:"dependency"`
		}
		if err := toml.Unmarshal([]byte(strings.Join(lines[b.start:b.end], "\n")), &parsed); err != nil {
			continue
		}
		if len(parsed.Dependency) == 1 && parsed.Dependency[0].Source == "store" && parsed.Dependency[0].Name == name {
			return &b
		}
	}
	return nil
}

func checked(lines []string, want []Dependency) ([]byte, error) {
	out := []byte(strings.Join(lines, "\n"))
	after, err := Parse(out)
	if err != nil {
		return nil, err
	}
	if len(after.Dependencies) != len(want) || (len(want) > 0 && !reflect.DeepEqual(after.Dependencies, want)) {
		return nil, errors.New("couldn't edit this manifest safely, change the dependency by hand")
	}
	return out, nil
}
