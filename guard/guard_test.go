// Package guard holds the release guard.
//
// The marketplace refuses plugins that ship or expose instructions aimed at
// coding agents, whether installed, printed or embedded in a binary. This test
// walks the whole repository and fails when such content reappears, so a
// release cannot regress into it.
package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// skipDirs are not part of a release.
var skipDirs = map[string]bool{
	".git": true,
	"bin":  true,
}

// forbiddenNames are filenames that are an instruction channel by convention,
// whatever they contain.
var forbiddenNames = []string{
	"agents.md",
	"skill.md",
	"claude.md",
	"agent.md",
}

// forbiddenDirs are the roots a plugin must never write to or vendor.
var forbiddenDirs = []string{
	".claude",
	".agents",
	"skills",
}

// directive matches prose addressed to an agent rather than to a person.
//
// The phrases are assembled from fragments so this file does not match itself.
var directive = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`for cod` + `ing agents`,
	`you are an ` + `agent`,
	`before ` + `acting`,
	`the ` + `agent must`,
	`instructions for ` + `agents`,
	`agent-` + `facing guide`,
	`guide an ` + `agent`,
	`an ` + `agent reads`,
	`print the ` + `guide`,
}, "|"))

// embedMarkdown catches a Go file embedding markdown into a binary, which is
// how a printed guide survives a deleted file.
var embedMarkdown = regexp.MustCompile(`go:embed\s+\S*\.md`)

// frontMatter catches a leading YAML block with a name and description, which
// is the shape that makes a markdown file auto-load as a skill.
var frontMatter = regexp.MustCompile(`(?s)\A---\r?\n.*?\bname:.*?\bdescription:.*?\r?\n---`)

func TestNoAgentInstructionSurface(t *testing.T) {
	root := ".."
	self, err := filepath.Abs("guard_test.go")
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			for _, bad := range forbiddenDirs {
				if strings.EqualFold(d.Name(), bad) {
					t.Errorf("%s: a plugin must not ship a %s directory", path, bad)
					return filepath.SkipDir
				}
			}
			return nil
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if abs == self {
			return nil
		}

		name := strings.ToLower(d.Name())
		for _, bad := range forbiddenNames {
			if name == bad {
				t.Errorf("%s: %s is an agent instruction channel", path, bad)
			}
		}

		switch filepath.Ext(name) {
		case ".md", ".go", ".qml", ".json", ".txt":
		default:
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)

		if m := directive.FindString(text); m != "" {
			t.Errorf("%s: reads as an instruction to an agent (%q)", path, m)
		}
		if m := embedMarkdown.FindString(text); m != "" {
			t.Errorf("%s: embeds markdown into a binary (%q)", path, m)
		}
		if frontMatter.MatchString(text) {
			t.Errorf("%s: has skill front matter", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var (
	manifestVersion = regexp.MustCompile(`"version"\s*:\s*"([^"]+)"`)
	makefileVersion = regexp.MustCompile(`(?m)^VERSION\s*\?=\s*(\S+)`)
)

// TestVersionsAgree keeps the manifest and the Makefile from drifting. The
// marketplace shows the manifest's version; nothing else checks they match.
func TestVersionsAgree(t *testing.T) {
	manifest, err := os.ReadFile("../manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	m := manifestVersion.FindSubmatch(manifest)
	if m == nil {
		t.Fatal("manifest.json has no version field")
	}

	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	mk := makefileVersion.FindSubmatch(makefile)
	if mk == nil {
		t.Fatal("the Makefile has no VERSION")
	}

	if string(m[1]) != string(mk[1]) {
		t.Errorf("manifest.json says %q, the Makefile says %q", m[1], mk[1])
	}
}

// helperFlags matches an invocation of the helper from QML. Only a command
// array opening with the helper counts: the staleness probe passes helperPath
// to find as an argument, which is not a call.
var (
	helperFlags = regexp.MustCompile(`\[\s*root\.helperPath\s*,\s*"(-{1,2}[a-z][a-z0-9-]*)"`)
	flagDecl    = regexp.MustCompile(`flag\.(?:Bool|String|Int|Duration)\("([a-z][a-z0-9-]*)"`)
	comment     = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// TestQMLFlagsExist fails when the panel passes a flag the helper does not
// declare. Deleting a flag and leaving the caller is silent: the panel still
// builds and lints, and the failure only shows as an error at runtime.
func TestQMLFlagsExist(t *testing.T) {
	src, err := os.ReadFile("../cmd/demarchy/main.go")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, m := range flagDecl.FindAllStringSubmatch(string(src), -1) {
		known[m[1]] = true
	}
	if len(known) == 0 {
		t.Fatal("cmd/demarchy/main.go: found no flags, the pattern has drifted")
	}

	entries, err := filepath.Glob("../*.qml")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := comment.ReplaceAllString(string(body), "")
		for _, m := range helperFlags.FindAllStringSubmatch(text, -1) {
			seen++
			if !known[strings.TrimLeft(m[1], "-")] {
				t.Errorf("%s: passes %q, which demarchy does not declare", path, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no helper invocations in QML, the pattern has drifted")
	}
}

// pinnedVerbs is every verb demarchy offers.
//
// Dispatch is split between a verb table and two switches, so a verb can be
// added without the usage text or anything else noticing. Pinning the set is
// what makes a removed verb stay removed: re-adding one fails here, by name.
var pinnedVerbs = []string{
	"add", "allow-http", "alerts", "arm", "catalogue", "check",
	"disallow-http", "disarm", "edit", "list", "purge", "remove",
}

var (
	preSwitch  = regexp.MustCompile(`verb\s*==\s*"([a-z][a-z0-9-]*)"`)
	switchCase = regexp.MustCompile(`(?m)^\s*case\s+"([a-z][a-z0-9-]*)?":`)
)

func TestSetupVerbsArePinned(t *testing.T) {
	found := map[string]bool{}
	for _, file := range []string{
		"../cmd/demarchy/verbs.go",
		"../cmd/demarchy/alerts.go",
	} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := comment.ReplaceAllString(string(body), "")
		for _, m := range preSwitch.FindAllStringSubmatch(text, -1) {
			found[m[1]] = true
		}
		// Only the dispatch switch names verbs. Both files switch on other
		// things too, and their cases are statuses, not commands.
		rest := text
		for {
			at := strings.Index(rest, "switch verb {")
			if at < 0 {
				break
			}
			rest = rest[at+len("switch verb {"):]
			block := rest
			if end := strings.Index(block, "\n\t}"); end >= 0 {
				block = block[:end]
			}
			for _, m := range switchCase.FindAllStringSubmatch(block, -1) {
				found[m[1]] = true
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("found no verbs, the dispatch pattern has drifted")
	}

	// The verbs map is the gate: a verb dispatched but not listed there is
	// unreachable, and one listed but not dispatched answers "unknown
	// command". Both are silent, so both are checked.
	body, err := os.ReadFile("../cmd/demarchy/verbs.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	at := strings.Index(text, "var verbs = map[string]bool{")
	if at < 0 {
		t.Fatal("cmd/demarchy/verbs.go: no verbs map, the pattern has drifted")
	}
	table := text[at:]
	table = table[:strings.Index(table, "}")]
	gated := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z][a-z0-9-]*)":\s*true`).FindAllStringSubmatch(table, -1) {
		gated[m[1]] = true
	}
	for v := range found {
		if !gated[v] {
			t.Errorf("verb %q is dispatched but missing from the verbs map, so it is unreachable", v)
		}
	}
	for v := range gated {
		if !found[v] {
			t.Errorf("verb %q is in the verbs map but nothing dispatches it", v)
		}
	}

	pinned := map[string]bool{}
	for _, v := range pinnedVerbs {
		pinned[v] = true
		if !found[v] {
			t.Errorf("verb %q is pinned but no longer dispatched", v)
		}
	}
	for v := range found {
		if !pinned[v] {
			t.Errorf("demarchy offers %q, which is not pinned", v)
		}
	}
}

// outbound lists, per package, the symbols that open a connection outward.
var outbound = map[string][]string{
	"net/http":   {"Get", "Head", "Post", "PostForm", "NewRequest", "NewRequestWithContext", "DefaultClient", "DefaultTransport", "Client", "Transport"},
	"net":        {"Dial", "DialTimeout", "DialTCP", "DialUDP", "DialIP", "DialUnix", "Dialer"},
	"crypto/tls": {"Dial", "DialWithDialer", "Dialer"},
}

// outboundFiles are the only files that may use them. dcrpulse is the single
// destination demarchy has, and one file reaches it.
var outboundFiles = []string{
	"internal/dcr/mcp.go",
}

// TestOutboundNetworkIsConfined fails when a dialling symbol appears outside
// the files allowed to use one. Each allowed file must itself use one, so a
// renamed file or a detector that stopped matching fails here rather than
// passing quietly.
func TestOutboundNetworkIsConfined(t *testing.T) {
	root := ".."
	hits := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for pkg, symbols := range outbound {
			short := pkg[strings.LastIndex(pkg, "/")+1:]
			if !strings.Contains(text, `"`+pkg+`"`) {
				continue
			}
			for _, sym := range symbols {
				if strings.Contains(text, short+"."+sym) {
					rel, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					hits[filepath.ToSlash(rel)] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	allowed := map[string]bool{}
	for _, file := range outboundFiles {
		allowed[file] = true
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			t.Errorf("%s is missing: update outboundFiles if it moved", file)
			continue
		}
		if !hits[file] {
			t.Errorf("%s no longer dials: the detector or the file has changed", file)
		}
	}
	for file := range hits {
		if !allowed[file] {
			t.Errorf("%s dials outward, which only %v may do", file, outboundFiles)
		}
	}
}
