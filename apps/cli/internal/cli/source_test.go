package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// sourceEvents splits the events of a source command into the one source
// event and the source_skill events.
func sourceEvents(t *testing.T, events []jsonEvent) (jsonEvent, []jsonEvent) {
	t.Helper()
	var src jsonEvent
	var skills []jsonEvent
	for _, e := range events {
		switch e["type"] {
		case "source":
			if src != nil {
				t.Fatalf("two source events: %v and %v", src, e)
			}
			src = e
		case "source_skill":
			skills = append(skills, e)
		}
	}
	if src == nil {
		t.Fatalf("no source event in %v", events)
	}
	return src, skills
}

func TestSourceAddNormalisesEveryURLForm(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, v1, head := h.standardSource(true)
	h.rewrite(s, "https://github.com/owner/repo", "https://gitlab.com/group/sub/repo", "ssh://git@github.com/owner/repo")

	tests := []struct {
		in      string
		url     string
		subpath string
		pin     string
		commit  string
		skills  float64
	}{
		{"owner/repo", "https://github.com/owner/repo", "", "", head, 2},
		{"owner/repo/skills/alpha", "https://github.com/owner/repo", "skills/alpha", "", head, 1},
		{"owner/repo#v1", "https://github.com/owner/repo", "", "v1", v1, 2},
		{"https://github.com/owner/repo", "https://github.com/owner/repo", "", "", head, 2},
		{"https://github.com/owner/repo.git/", "https://github.com/owner/repo", "", "", head, 2},
		{"https://github.com/owner/repo/tree/main/skills", "https://github.com/owner/repo", "skills", "main", head, 2},
		{"https://github.com/owner/repo/tree/v1/skills/beta", "https://github.com/owner/repo", "skills/beta", "v1", v1, 1},
		{"https://gitlab.com/group/sub/repo/-/tree/main/skills/alpha", "https://gitlab.com/group/sub/repo", "skills/alpha", "main", head, 1},
		{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo", "", "", head, 2},
		{"ssh://git@github.com/owner/repo.git#v1", "ssh://git@github.com/owner/repo", "", "v1", v1, 2},
		{"https://someone:s3cret-token@github.com/owner/repo#main", "https://github.com/owner/repo", "", "main", head, 2},
		{s.url + "#v1", s.url, "", "v1", v1, 2},
		{"file://localhost" + s.gitDir + "/", s.url, "", "", head, 2},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			out := h.run("--json", "source", "add", tt.in)
			equal(t, "exit", out.exit, 0)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"source", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v\n%s", got, want, out.stderr)
			}
			src := events[0]
			equal(t, "url", src["url"], tt.url)
			equal(t, "id", src["id"], source.ID(tt.url))
			equal(t, "subpath", src["subpath"], anyOrNil(tt.subpath))
			equal(t, "pin", src["pin"], anyOrNil(tt.pin))
			equal(t, "commit", src["commit"], tt.commit)
			equal(t, "skills", src["skills"], tt.skills)
			if _, err := time.Parse(time.RFC3339, src["last_fetched"].(string)); err != nil {
				t.Errorf("last_fetched %v: %v", src["last_fetched"], err)
			}
			if strings.Contains(tt.in, "s3cret") {
				logs := h.events(out.stderr)
				if len(logs) != 1 || logs[0]["level"] != "warn" || !strings.Contains(logs[0]["message"].(string), "credential helper") {
					t.Errorf("stderr = %q, want one warning naming the credential helper", out.stderr)
				}
			} else {
				equal(t, "stderr", out.stderr, "")
			}
		})
	}

	// The token appears nowhere: not in the settings, the account repo
	// configuration, or the output of any command.
	settings, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := h.accountGit("config", "--list", "--local")
	list := h.run("source", "list")
	for what, text := range map[string]string{"settings.json": string(settings), "account repo config": config, "source list": list.stdout + list.stderr} {
		if strings.Contains(text, "s3cret") || strings.Contains(text, "someone") {
			t.Errorf("%s carries the credential:\n%s", what, text)
		}
	}
	// One entry per canonical URL, whatever forms were added, pinned as the last add said.
	file := readSettingsFile(t, h)
	sources := file["sources"].([]any)
	if len(sources) != 4 {
		t.Fatalf("sources = %v, want four entries", sources)
	}
	equal(t, "sources[1].url", sources[1].(map[string]any)["url"], "https://github.com/owner/repo")
	equal(t, "sources[1].pin", sources[1].(map[string]any)["pin"], "main")
	if _, ok := sources[1].(map[string]any)["alias"]; ok {
		t.Errorf("alias written although unused: %v", sources[1])
	}
}

func anyOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestSourceAddFetchesBloblessAndSkillFilesInOneBatch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, v1, head := h.standardSource(true)
	id := source.ID(s.url)

	out := h.run("--verbose", "source", "add", s.url)
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ added "+s.url+" at "+head[:7]+": 2 skills\n")
	equal(t, "fetches", fetches(out.stderr), 2)

	// The remote: promisor, blob:none, no tags, and one refspec recording
	// the pin, onto the staging ref rather than the source ref, since the
	// source ref is published by a fetch that is whole and never fetched into.
	for key, want := range map[string]string{
		"url":                s.url,
		"fetch":              "+HEAD:" + source.StagingRef(id),
		"tagopt":             "--no-tags",
		"promisor":           "true",
		"partialclonefilter": "blob:none",
	} {
		equal(t, "remote.src-"+id+"."+key, h.accountGit("config", "--get", "remote.src-"+id+"."+key), want)
	}
	equal(t, "ref", h.accountGit("rev-parse", "refs/agentx/sources/"+id), head)
	equal(t, "tags", h.accountGit("for-each-ref", "refs/tags"), "")

	// Every SKILL.md blob is present; every other blob is absent.
	present, missing := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(h.accountGit("rev-list", "--objects", "--missing=print", "refs/agentx/sources/"+id), "\n") {
		oid, path, _ := strings.Cut(line, " ")
		if strings.HasPrefix(oid, "?") {
			missing[oid[1:]] = true
		} else if path != "" {
			present[path] = true
		}
	}
	for _, line := range strings.Split(h.accountGit("ls-tree", "-r", head), "\n") {
		meta, path, _ := strings.Cut(line, "\t")
		oid := strings.Fields(meta)[2]
		if strings.HasSuffix(path, "SKILL.md") {
			if !present[path] || missing[oid] {
				t.Errorf("%s is not present after the add", path)
			}
		} else if present[path] || !missing[oid] {
			t.Errorf("%s was fetched although only SKILL.md blobs should be", path)
		}
	}

	// The settings entry and the version bump.
	file := readSettingsFile(t, h)
	sources := file["sources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("sources = %v", sources)
	}
	entry := sources[0].(map[string]any)
	equal(t, "url", entry["url"], s.url)
	if _, ok := entry["pin"]; ok {
		t.Errorf("pin written for an unpinned source: %v", entry)
	}
	if _, err := time.Parse(time.RFC3339, entry["last_fetched"].(string)); err != nil {
		t.Errorf("last_fetched %v: %v", entry["last_fetched"], err)
	}
	equal(t, "version", readVersion(t, h), 2) // the account repo's creation, then the settings

	// Adding again re-fetches and keeps one entry; the confirmation says so.
	out = h.run("--verbose", "source", "add", s.url+"#v1")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "✓ re-fetched "+s.url+" pinned to v1, now at "+v1[:7]+", was "+head[:7])
	equal(t, "fetches", fetches(out.stderr), 2)
	equal(t, "refspec", h.accountGit("config", "--get", "remote.src-"+id+".fetch"), "+v1:"+source.StagingRef(id))
	file = readSettingsFile(t, h)
	sources = file["sources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("sources = %v", sources)
	}
	equal(t, "pin", sources[0].(map[string]any)["pin"], "v1")
	equal(t, "version", readVersion(t, h), 3)
}

func TestSourceAddFallsBackToFullFetch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, head := h.standardSource(false) // no uploadpack.allowFilter: the server ignores the filter
	id := source.ID(s.url)

	out := h.run("--json", "source", "add", s.url+"/skills")
	equal(t, "exit", out.exit, 0)
	src, _ := sourceEvents(t, h.events(out.stdout))
	equal(t, "commit", src["commit"], head)
	equal(t, "skills", src["skills"], float64(2))
	equal(t, "ref", h.accountGit("rev-parse", "refs/agentx/sources/"+id), head)
	if missing := strings.Count(h.accountGit("rev-list", "--objects", "--missing=print", "refs/agentx/sources/"+id), "\n?"); missing != 0 {
		t.Errorf("%d objects missing after a full fetch", missing)
	}
	// The listing is complete afterwards, offline.
	out = h.run("--json", "source", "skills", id)
	equal(t, "exit", out.exit, 0)
	_, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 2)
}

// TestSourceAddRefetchesWhenTheServerRefusesSingleObjects covers the last
// resort of source.Fetch. The server here really filters, so the first
// fetch lands the commit and its trees with every blob missing; the
// by-object-id batch that should fill the SKILL.md blobs in is refused, the
// way a server that serves a filtered fetch but no want for an
// unadvertised object refuses it. Only the --refetch --no-filter fetch can
// complete the add, and everything the source holds must be present once it
// has, so that no later listing reads the network.
func TestSourceAddRefetchesWhenTheServerRefusesSingleObjects(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, head := h.standardSource(true) // uploadpack.allowFilter: the filter is honoured, the blobs stay behind
	id := source.ID(s.url)
	refuseObjectFetch(t, h)

	out := h.run("--verbose", "source", "add", s.url+"/skills")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "✓ added "+s.url+" at "+head[:7]+": 2 skills under skills")
	// Three fetches: the filtered one, the refused batch, the full re-fetch.
	equal(t, "fetches", fetches(out.stderr), 3)
	// The full re-fetch lands on the staging ref like the blobless one: the
	// source ref moves only once every blob is here.
	refetch := regexp.MustCompile(`--no-show-forced-updates --refmap= --refetch --no-filter src-` + id + ` \+HEAD:` + source.StagingRefPrefix + `[0-9a-f]{16}/` + id + `\n`)
	if !refetch.MatchString(out.stderr) {
		t.Errorf("stderr has no full re-fetch onto a staging ref of the fetch's own:\n%s", out.stderr)
	}

	// The re-fetch took everything, although the remote is still a promisor
	// with a blob:none filter, so the account repo is whole.
	equal(t, "ref", h.accountGit("rev-parse", "refs/agentx/sources/"+id), head)
	equal(t, "missing objects", h.missingObjects("refs/agentx/sources/"+id), 0)

	// The listing the add printed is the real one, and every later listing
	// is answered from the account repo alone.
	out = h.run("--verbose", "--json", "source", "skills", id)
	equal(t, "exit", out.exit, 0)
	src, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "commit", src["commit"], head)
	equal(t, "skills", len(skills), 2)
	equal(t, "skills[0]", skills[0]["name"], "alpha")
	equal(t, "skills[1]", skills[1]["name"], "beta")
	if n := fetches(out.stderr); n != 0 {
		t.Errorf("source skills ran %d git fetches; it must read the account repo alone", n)
	}
}

// TestSourceAddPeelsAnAnnotatedTagPin: a pin naming an annotated tag puts
// the tag object itself on the source ref. Both the add and the list must
// report the commit it peels to, never the tag's own id.
func TestSourceAddPeelsAnAnnotatedTagPin(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("annotated", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	head := s.commit("one")
	s.annotatedTag("v2", "release two")
	id := source.ID(s.url)

	out := h.run("--json", "source", "add", s.url+"#v2")
	equal(t, "exit", out.exit, 0)
	src, _ := sourceEvents(t, h.events(out.stdout))
	equal(t, "commit", src["commit"], head)
	equal(t, "skills", src["skills"], float64(1))

	// The ref holds the tag object, so the peel is the code's own doing.
	ref := h.accountGit("rev-parse", "refs/agentx/sources/"+id)
	equal(t, "ref type", h.accountGit("cat-file", "-t", ref), "tag")
	if ref == head {
		t.Fatalf("the source ref is the commit itself; the test no longer covers peeling")
	}

	events := h.events(h.run("--json", "source", "list").stdout)
	equal(t, "list commit", events[0]["commit"], head)
	equal(t, "list pin", events[0]["pin"], "v2")

	out = h.run("--json", "source", "skills", id)
	equal(t, "exit", out.exit, 0)
	src, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills commit", src["commit"], head)
	equal(t, "skills", len(skills), 1)
}

// TestSourceAddKeepsAnAliasOnReAdd: nothing writes alias yet, but it is a
// second URL mapped onto the canonical one, and a re-add rewrites the whole
// entry. The alias must be carried across, not dropped.
func TestSourceAddKeepsAnAliasOnReAdd(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, v1, head := h.standardSource(true)
	h.mustRun("source", "add", s.url)

	const alias = "https://github.com/owner/repo"
	err := home.Mutate(h.agentx, nil, func() error {
		settings, err := home.LoadSettings(h.agentx)
		if err != nil {
			return err
		}
		at := settings.FindSource(s.url)
		if at < 0 {
			return fmt.Errorf("the settings hold no entry for %s", s.url)
		}
		entry := settings.Sources[at]
		entry.Alias = alias
		settings.SetSource(entry)
		return home.SaveSettings(h.agentx, settings)
	})
	if err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "source", "add", s.url+"#v1")
	equal(t, "exit", out.exit, 0)
	src, _ := sourceEvents(t, h.events(out.stdout))
	equal(t, "alias", src["alias"], alias)
	equal(t, "commit", src["commit"], v1)
	// The pin took the ref back to v1, which is a move like any other.
	equal(t, "previous_commit", src["previous_commit"], head)
	entry := readSettingsFile(t, h)["sources"].([]any)[0].(map[string]any)
	equal(t, "settings alias", entry["alias"], alias)
	equal(t, "settings pin", entry["pin"], "v1")

	// The listing carries it too, and so does a fetch, which rewrites the
	// entry's last_fetched and nothing else.
	equal(t, "list alias", h.events(h.run("--json", "source", "list").stdout)[0]["alias"], alias)
	fetched, _ := sourceEvents(t, h.events(h.run("--json", "source", "fetch", s.url).stdout))
	equal(t, "fetch alias", fetched["alias"], alias)
	equal(t, "fetch pin", fetched["pin"], "v1")
	equal(t, "fetch commit", fetched["commit"], v1)
	equal(t, "fetch previous_commit", fetched["previous_commit"], nil)
	entry = readSettingsFile(t, h)["sources"].([]any)[0].(map[string]any)
	equal(t, "settings alias after the fetch", entry["alias"], alias)
	equal(t, "settings pin after the fetch", entry["pin"], "v1")
}

func TestSourceSkills(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("nested", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "n\n"})
	s.skill("skills/alpha/nested", "alpha-nested", "Nested inside alpha", nil)
	s.skill("skills/beta", "", "", nil) // no name: the directory names it
	s.skill("skills/.hidden/secret", "secret", "Hidden", nil)
	s.skill("skills/node_modules/pkg", "pkg", "A dependency", nil)
	s.skill("tools/gamma", "gamma", "Outside skills", nil)
	s.write("skills/README.md", "not a skill\n")
	s.write("docs/SKILL.md.txt", "not a skill either\n")
	head := s.commit("skills")
	id := source.ID(s.url)

	out := h.run("--json", "source", "add", s.url)
	equal(t, "exit", out.exit, 0)
	src, _ := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", src["skills"], float64(4))

	out = h.run("--json", "source", "skills", s.url)
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"source", "source_skill", "source_skill", "source_skill", "source_skill", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	src, skills := sourceEvents(t, events)
	equal(t, "source.id", src["id"], id)
	equal(t, "source.commit", src["commit"], head)
	equal(t, "source.skills", src["skills"], float64(4))
	want := []jsonEvent{
		{"type": "source_skill", "schema_version": float64(1), "source": id, "subpath": "skills/alpha", "name": "alpha", "description": "The first skill", "tree": s.tree("skills/alpha")},
		{"type": "source_skill", "schema_version": float64(1), "source": id, "subpath": "skills/alpha/nested", "name": "alpha-nested", "description": "Nested inside alpha", "tree": s.tree("skills/alpha/nested")},
		{"type": "source_skill", "schema_version": float64(1), "source": id, "subpath": "skills/beta", "name": "beta", "description": "", "tree": s.tree("skills/beta")},
		{"type": "source_skill", "schema_version": float64(1), "source": id, "subpath": "tools/gamma", "name": "gamma", "description": "Outside skills", "tree": s.tree("tools/gamma")},
	}
	if !reflect.DeepEqual(skills, want) {
		t.Errorf("skills = %v\nwant %v", skills, want)
	}

	// Scoped to a subpath, by id or URL, and to one skill directory.
	for _, arg := range []string{s.url + "/skills", s.url + "/skills/"} {
		out = h.run("--json", "source", "skills", arg)
		equal(t, "exit", out.exit, 0)
		src, skills = sourceEvents(t, h.events(out.stdout))
		equal(t, "subpath", src["subpath"], "skills")
		equal(t, "skills", len(skills), 3)
	}
	out = h.run("--json", "source", "skills", s.url+"/skills/alpha/nested")
	equal(t, "exit", out.exit, 0)
	_, skills = sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 1)
	equal(t, "nested subpath", skills[0]["subpath"], "skills/alpha/nested")

	// The text listing.
	out = h.run("source", "skills", id)
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "4 skills in "+s.url+" at "+head[:7]+"\n"+
		"  alpha         skills/alpha         The first skill\n"+
		"  alpha-nested  skills/alpha/nested  Nested inside alpha\n"+
		"  beta          skills/beta\n"+
		"  gamma         tools/gamma          Outside skills\n")
	out = h.run("source", "skills", s.url+"/tools")
	equal(t, "stdout", out.stdout, "1 skill in "+s.url+" under tools at "+head[:7]+"\n  gamma  tools/gamma  Outside skills\n")

	// Nothing under a directory without skills, and errors.
	out = h.run("--json", "source", "skills", s.url+"/docs")
	equal(t, "exit", out.exit, 0)
	src, skills = sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", src["skills"], float64(0))
	equal(t, "skills", len(skills), 0)
	tests := []struct {
		name string
		args []string
		exit int
		code string
		hint string
	}{
		{"subpath missing", []string{"source", "skills", s.url + "/nope"}, 5, "not_found", "directory"},
		{"subpath is a file", []string{"source", "skills", s.url + "/skills/README.md"}, 5, "not_found", "directory"},
		{"unknown id", []string{"source", "skills", "0123456789abcdef"}, 5, "not_found", "agentx source list"},
		{"unknown url", []string{"source", "skills", "owner/other"}, 5, "not_found", "agentx source add https://github.com/owner/other"},
		{"other pin", []string{"source", "skills", s.url + "#v2"}, 1, "usage", "agentx source add " + s.url + "#v2"},
		{"bad url", []string{"source", "skills", "nope"}, 1, "usage", "owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := h.run(append([]string{"--json"}, tt.args...)...)
			equal(t, "exit", out.exit, tt.exit)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			equal(t, "error.code", events[0]["code"], tt.code)
			contains(t, "error.hint", events[0]["hint"].(string), tt.hint)
		})
	}
}

func TestSourceListAndRemove(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("source", "list")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "No sources. Add one with agentx source add <url>.\n")
	out = h.run("--json", "source", "list")
	if got, want := h.types(h.events(out.stdout)), []string{"result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	a, v1, _ := h.standardSource(true)
	b := h.newSourceRepo("beta", true)
	b.skill("one", "one", "One", nil)
	bHead := b.commit("one")
	equal(t, "exit", h.run("source", "add", a.url+"#v1").exit, 0)
	equal(t, "exit", h.run("source", "add", b.url).exit, 0)
	aID, bID := source.ID(a.url), source.ID(b.url)

	out = h.run("--json", "source", "list")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"source", "source", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	// Sorted by URL: beta.git before skills.git.
	equal(t, "0.url", events[0]["url"], b.url)
	equal(t, "0.id", events[0]["id"], bID)
	equal(t, "0.commit", events[0]["commit"], bHead)
	equal(t, "0.pin", events[0]["pin"], nil)
	equal(t, "0.skills", events[0]["skills"], nil)
	equal(t, "1.url", events[1]["url"], a.url)
	equal(t, "1.pin", events[1]["pin"], "v1")
	equal(t, "1.commit", events[1]["commit"], v1)
	// Each source carries its own fetch time: two adds a second apart must
	// not be asserted against one timestamp.
	bFetched := events[0]["last_fetched"].(string)
	aFetched := events[1]["last_fetched"].(string)

	out = h.run("source", "list")
	equal(t, "exit", out.exit, 0)
	lines := strings.Split(strings.TrimSuffix(out.stdout, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "2 sources" {
		t.Fatalf("source list printed:\n%s", out.stdout)
	}
	if got, want := strings.Fields(lines[1]), []string{b.url, "(unpinned)", bHead[:7], bFetched, bID}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 1 = %q, want %q", got, want)
	}
	if got, want := strings.Fields(lines[2]), []string{a.url, "v1", v1[:7], aFetched, aID}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 2 = %q, want %q", got, want)
	}
	contains(t, "stdout", out.stdout, "  "+b.url+"  ")

	// Remove by id: the entry, the remote and the ref go; the other source stays.
	out = h.run("--json", "source", "remove", aID)
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	if got, want := h.types(events), []string{"source", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "removed.url", events[0]["url"], a.url)
	if _, err := h.accountGitErr("config", "--get", "remote.src-"+aID+".url"); err == nil {
		t.Error("the remote of the removed source is still configured")
	}
	equal(t, "refs", h.accountGit("for-each-ref", "--format=%(refname)", "refs/agentx/sources/"), "refs/agentx/sources/"+bID)
	sources := readSettingsFile(t, h)["sources"].([]any)
	if len(sources) != 1 || sources[0].(map[string]any)["url"] != b.url {
		t.Errorf("sources after remove = %v", sources)
	}
	equal(t, "version", readVersion(t, h), 4) // the repo's creation, two adds, one removal

	// Remove by URL, with text output; then nothing is left.
	out = h.run("source", "remove", b.url)
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ removed "+b.url+"\n")
	equal(t, "refs", h.accountGit("for-each-ref", "refs/agentx/sources/"), "")
	equal(t, "config", strings.Contains(h.accountGit("config", "--list", "--local"), "remote.src-"), false)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 0)

	// Removing what is not there is exit 5 and no mutation.
	for _, arg := range []string{aID, a.url, "owner/repo"} {
		out = h.run("--json", "source", "remove", arg)
		equal(t, "exit", out.exit, 5)
		equal(t, "error.code", h.events(out.stdout)[0]["code"], "not_found")
	}
	equal(t, "version", readVersion(t, h), 5)

	// Adding again after a removal fetches the source anew and lists its skills.
	out = h.run("--verbose", "source", "add", a.url)
	equal(t, "exit", out.exit, 0)
	equal(t, "fetches", fetches(out.stderr), 2)
	if h.accountGit("rev-parse", "refs/agentx/sources/"+aID) != a.run("rev-parse", "HEAD") {
		t.Error("the re-added source is not at the source's head")
	}
	out = h.run("--json", "source", "skills", aID)
	equal(t, "exit", out.exit, 0)
	_, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 2)
}

func TestSourceAddErrors(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	missing := h.newSourceRepo("gone", true)
	if err := os.RemoveAll(missing.gitDir); err != nil {
		t.Fatal(err)
	}
	h.rewrite(missing, "https://github.com/owner/private")

	tests := []struct {
		name    string
		arg     string
		exit    int
		code    string
		message string
		hint    string
	}{
		{"unreachable", missing.url, 3, "source", "does not appear to be a git repository", "credential helper"},
		{"unreachable with a token", "https://me:s3cret@github.com/owner/private", 3, "source", "https://github.com/owner/private: ", "dropped"},
		{"missing ref", s.url + "#nope", 5, "not_found", `has no ref "nope"`, "pin a branch, tag or commit"},
		{"missing subpath", s.url + "/nope", 5, "not_found", `subpath not in the source: "nope"`, "directory"},
		{"bad form", "https://github.com/owner/repo/blob/main/SKILL.md", 1, "usage", "not a source URL", "owner/repo"},
		{"empty ref", "owner/repo#", 1, "usage", "does not end in a ref", ""},
		{"two refs", "https://github.com/owner/repo/tree/main/x#dev", 1, "usage", "the URL names ref", ""},
		{"scheme", "ftp://example.com/repo", 1, "usage", "unsupported scheme", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := h.run("--json", "source", "add", tt.arg)
			equal(t, "exit", out.exit, tt.exit)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			equal(t, "error.code", events[0]["code"], tt.code)
			contains(t, "error.message", events[0]["message"].(string), tt.message)
			contains(t, "error.hint", events[0]["hint"].(string), tt.hint)
			if strings.Contains(out.stdout+out.stderr, "s3cret") {
				t.Errorf("the token leaked into the output:\n%s%s", out.stdout, out.stderr)
			}
		})
	}
	// No failed add left an entry, a remote or a ref behind.
	if _, err := os.Stat(filepath.Join(h.agentx, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("a failed add wrote settings.json: %v", err)
	}
	if config := h.accountGit("config", "--list", "--local"); strings.Contains(config, "remote.") {
		t.Errorf("a failed add left a remote behind:\n%s", config)
	}
	equal(t, "refs", h.accountGit("for-each-ref", "refs/agentx/"), "")

	// A failed re-fetch of a known source keeps what it had.
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit", h.run("--json", "source", "add", s.url+"#nope").exit, 5)
	equal(t, "ref kept", h.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)), s.run("rev-parse", "HEAD"))
	equal(t, "refspec kept", h.accountGit("config", "--get", "remote.src-"+source.ID(s.url)+".fetch"), "+HEAD:"+source.StagingRef(source.ID(s.url)))
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 1)
	equal(t, "exit", h.run("--json", "source", "skills", s.url).exit, 0)
	out := h.run("source", "add", missing.url)
	equal(t, "exit", out.exit, 3)
	contains(t, "stderr", out.stderr, "error: "+missing.url+": git fetch: ")
	contains(t, "stderr", out.stderr, "hint: check the URL")
}

func TestSourceCommandsRespectTheLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	lock := filepath.Join(h.agentx, "lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"source", "add", s.url}, {"source", "fetch", "--all"}, {"source", "remove", s.url}} {
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, "exit", out.exit, 7)
		equal(t, "error.code", h.events(out.stdout)[0]["code"], "locked")
	}
	// Reads are not blocked.
	for _, args := range [][]string{{"source", "list"}, {"source", "skills", s.url}} {
		equal(t, strings.Join(args, " "), h.run(args...).exit, 0)
	}
	equal(t, "version", readVersion(t, h), 2)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 1)
}

func TestSourceHelpAndUsage(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("source")
	equal(t, "exit", out.exit, 0)
	for _, sub := range []string{"add", "fetch", "list", "skills", "remove"} {
		contains(t, "stdout", out.stdout, sub)
	}
	out = h.run("--json", "source")
	equal(t, "exit", out.exit, 1)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "usage")
	out = h.run("--json", "source", "add")
	equal(t, "exit", out.exit, 1)
}

// TestSourceAddNeverEchoesACredential covers every refusal that names what
// it was given: a token in the URL must not reach stdout, stderr or an
// event, whichever way the input is wrong.
func TestSourceAddNeverEchoesACredential(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const secret = "ghp_S3CRETT0KEN"
	for _, arg := range []string{
		"https://user:" + secret + "@github.com",
		"https://" + secret + "@github.com/owner",
		"https://user:" + secret + "@host:notaport/owner/repo",
		"https://user:" + secret + "@github.com/owner/repo/blob/main/SKILL.md",
		"https://user:" + secret + "@github.com/owner/repo#",
		"https://user:" + secret + "@github.com/owner/../repo",
		"ftp://user:" + secret + "@example.com/repo",
		"user:" + secret + "@example.com/owner/repo",
		"https://u:p#" + secret + "@github.com/owner/repo",
	} {
		t.Run(arg, func(t *testing.T) {
			out := h.run(append([]string{"--json", "source", "add"}, arg)...)
			if out.exit == 0 {
				t.Fatalf("exit 0 for %q", arg)
			}
			if strings.Contains(out.stdout, secret) || strings.Contains(out.stderr, secret) {
				t.Errorf("the token reached the output:\nstdout: %s\nstderr: %s", out.stdout, out.stderr)
			}
			events := h.events(out.stdout)
			if len(events) == 0 || events[0]["type"] != "error" {
				t.Fatalf("events = %v", events)
			}
			// A message either leaves the input out or names it redacted;
			// what it may never do is repeat the userinfo it was given.
			if msg := events[0]["message"].(string); strings.Contains(msg, "@") {
				contains(t, "error.message", msg, "***@")
			}
		})
	}
}

// TestSourceSkillsReadsEveryPartOfTheSourceOffline pins what a subpath on
// the add does not do: it scopes that listing only. Every later listing,
// whether wider, narrower or by id, is answered from the account repo, so
// none of them may need the network or fail.
func TestSourceSkillsReadsEveryPartOfTheSourceOffline(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("wide", true) // a server that really filters
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "n\n"})
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.skill("tools/gamma", "gamma", "Outside skills", nil)
	s.skill(".hidden/secret", "secret", "Named outright", nil)
	s.commit("skills")
	id := source.ID(s.url)

	out := h.run("--verbose", "source", "add", s.url+"/skills/alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "1 skill under skills/alpha")
	adds := fetches(out.stderr)

	// Every listing below must spawn no fetch at all: the add brought every
	// SKILL.md of the source, not only the one under its subpath.
	for _, tt := range []struct {
		arg   string
		count int
	}{
		{id, 3},
		{s.url, 3},
		{s.url + "/skills", 2},
		{s.url + "/tools", 1},
		{s.url + "/skills/beta", 1},
		{s.url + "/.hidden", 1}, // skipped in a wider listing, listed when named
	} {
		out := h.run("--verbose", "--json", "source", "skills", tt.arg)
		equal(t, "exit "+tt.arg, out.exit, 0)
		_, skills := sourceEvents(t, h.events(out.stdout))
		equal(t, "skills in "+tt.arg, len(skills), tt.count)
		if n := fetches(out.stderr); n != 0 {
			t.Errorf("source skills %s ran %d git fetches; it must read the account repo alone", tt.arg, n)
		}
	}
	equal(t, "fetches during the add", adds, 2)
}

// TestSourceRemoveKeepsTheEntryWhenGitFails and the orphan it can clean up:
// the settings entry is the only thing that names a source, so it may not be
// dropped while the remote is still there.
func TestSourceRemoveKeepsTheEntryWhenGitFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	id := source.ID(s.url)

	// git cannot write the account repo's config while this lock file exists.
	lock := filepath.Join(h.agentx, "account.git", "config.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := h.run("--json", "source", "remove", s.url)
	if out.exit == 0 {
		t.Fatal("remove reported success while git could not write")
	}
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "account_repo")
	if sources := readSettingsFile(t, h)["sources"].([]any); len(sources) != 1 {
		t.Fatalf("the entry was dropped while the remote remained: %v", sources)
	}

	// With git writable again the same command finishes the job.
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	equal(t, "remove", h.run("source", "remove", s.url).exit, 0)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 0)
	equal(t, "refs", h.accountGit("for-each-ref", "refs/agentx/sources/"), "")

	// An orphan the other way round: git state with no entry, which only
	// remove can reach.
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	err := home.Mutate(h.agentx, nil, func() error {
		settings, err := home.LoadSettings(h.agentx)
		if err != nil {
			return err
		}
		settings.RemoveSource(s.url)
		return home.SaveSettings(h.agentx, settings)
	})
	if err != nil {
		t.Fatal(err)
	}
	out = h.run("source", "remove", id)
	equal(t, "orphan remove exit", out.exit, 0)
	equal(t, "refs", h.accountGit("for-each-ref", "refs/agentx/sources/"), "")
	if config := h.accountGit("config", "--list", "--local"); strings.Contains(config, "src-"+id) {
		t.Errorf("the orphaned remote is still configured:\n%s", config)
	}
}

// TestConcurrentSourceAdds proves two adds cannot leave a half-written
// remote behind: git config fails rather than waiting for its own lock, so
// the write is serialised by agentx's lock instead.
// TestSourceTellsALocalGitFailureFromAnUnfetchedSource covers the two ways
// a listing can find nothing: an account repo git cannot read, which is
// exit code 8 in every source command, and a source the account repo holds
// no ref for, which stays exit code 5. Reporting the first as the second
// sends the reader to `source add`, which fails differently again.
func TestSourceTellsALocalGitFailureFromAnUnfetchedSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	id := source.ID(s.url)

	// The ref alone is gone: the source was never fetched on this machine.
	h.accountGit("update-ref", "-d", source.Ref(id))
	out := h.run("--json", "source", "skills", s.url)
	equal(t, "exit", out.exit, 5)
	events := h.events(out.stdout)
	equal(t, "error.code", events[0]["code"], "not_found")
	contains(t, "error.message", events[0]["message"].(string), "source not fetched")
	contains(t, "error.hint", events[0]["hint"].(string), "agentx source add "+sourceAddArg(s.url, ""))
	equal(t, "list exit", h.run("source", "list").exit, 0) // the settings entry is still there

	// A packed-refs file git refuses to parse. rev-parse
	// --is-bare-repository still succeeds, so the account repo check passes
	// and each command meets the failure where it reads the refs.
	packed := filepath.Join(gitx.AccountRepoPath(h.agentx), "packed-refs")
	if err := os.WriteFile(packed, []byte("this is not a packed-refs line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.accountGitErr("for-each-ref", "refs/agentx/sources/"); err == nil {
		t.Fatal("the account repo is still readable; the test proves nothing")
	}
	for _, args := range [][]string{{"source", "list"}, {"source", "skills", s.url}} {
		what := strings.Join(args, " ")
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, "exit of "+what, out.exit, 8)
		events := h.events(out.stdout)
		equal(t, "error.code of "+what, events[0]["code"], "account_repo")
		hint, _ := events[0]["hint"].(string) // an account repo failure always carries one
		contains(t, "error.hint of "+what, hint, "agentx doctor")
	}
}

// TestSourceSkillsSanitisesUntrustedText covers a source that writes
// control characters into the text a listing prints. The rows stay rows,
// nothing drives the terminal, and the event carries what the source wrote.
func TestSourceSkillsSanitisesUntrustedText(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("evil", true)
	s.nastySkill("nasty")
	s.skill("plain", "plain", "A plain skill", nil)
	head := s.commit("skills")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	// The event is the source's own text: JSON escapes it, so a consumer
	// reads what the repository wrote.
	out := h.run("--json", "source", "skills", s.url)
	equal(t, "exit", out.exit, 0)
	_, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 2)
	equal(t, "raw name", skills[0]["name"], nastyName)
	equal(t, "raw description", skills[0]["description"], nastyDescription)

	// The text listing is one row per skill, two spaces of indent, and no
	// control character anywhere.
	out = h.run("source", "skills", s.url)
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "2 skills in "+s.url+" at "+head[:7]+"\n"+
		"  na sty [31mRED [0m  nasty  first line second line ]0;pwned and tab\n"+
		"  plain               plain  A plain skill\n")
	for _, r := range out.stdout {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the listing: %q in\n%q", r, out.stdout)
		}
	}

	// Painting still changes nothing but the sequences agentx adds: the
	// injected ones are gone before anything is painted, so stripping gives
	// the text a pipe receives, which is what Streams promises.
	painted := h.run("--color=on", "source", "skills", s.url)
	if got := escapes.ReplaceAllString(painted.stdout, ""); got != out.stdout {
		t.Errorf("painted output, escapes stripped, differs from plain:\n%q\n%q", got, out.stdout)
	}
}

// TestSourceAddSanitisesWhatTheRemoteSays covers first contact with an
// unknown repository. git relays a server's sideband messages, the
// "remote:" lines, to its own stderr as they arrive, agentx keeps the first
// line of that as the cause of the refusal, and the refusal is printed.
// CVE-2024-52005 is exactly that path: escape sequences in a sideband
// message drive the terminal of whoever ran the fetch. It matters most
// here, because 'source add <url>' is the command run before the user has
// seen anything of the repository and decided whether to trust it.
func TestSourceAddSanitisesWhatTheRemoteSays(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	hostileRemote(t, h)

	out := h.run("source", "add", "file:///nowhere.git")
	equal(t, "exit", out.exit, 3)
	// Defanged and still legible: what the server said is printed as the
	// ordinary text it is, which also shows that it tried.
	contains(t, "stderr", out.stderr, "git fetch: remote: [2K ]0;pwned hello from the server")
	for _, r := range out.stderr {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the terminal: %q in\n%q", r, out.stderr)
		}
	}

	// The event carries what the server sent, escaped by JSON: the rule is
	// the terminal's, not the consumer's.
	events := h.run("--json", "source", "add", "file:///nowhere.git")
	equal(t, "exit", events.exit, 3)
	message, _ := lastError(t, h.events(events.stdout))["message"].(string)
	contains(t, "error.message", message, "remote: \x1b[2K\x1b]0;pwned\ahello from the server")
}

// TestSourceSkillEventIsTheFrontmatterItself pins the boundary the whole
// sanitising rule stands on. Once a source is fetched, its files are on
// this machine, and the UI learns the state of those files from the JSON
// events alone: a source_skill event therefore carries the frontmatter byte
// for byte. Sanitising a value on its way into an event would leave the UI
// showing something the file on disk does not say, and until this test
// nothing in the suite would have failed for it.
func TestSourceSkillEventIsTheFrontmatterItself(t *testing.T) {
	t.Parallel()
	// The fixture is worth exactly what it carries, so it is checked first:
	// an escape, a carriage return, a newline and a bell.
	for _, char := range []struct {
		name string
		r    rune
	}{{"ESC", 0x1b}, {"CR", '\r'}, {"LF", '\n'}, {"BEL", 0x07}} {
		if !strings.ContainsRune(nastyName+nastyDescription, char.r) {
			t.Fatalf("the frontmatter fixture carries no %s", char.name)
		}
	}
	h := newHarness(t)
	s := h.newSourceRepo("verbatim", true)
	s.nastySkill("nasty")
	s.commit("one skill")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	out := h.run("--json", "source", "skills", s.url)
	equal(t, "exit", out.exit, 0)
	_, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 1)
	equal(t, "name", skills[0]["name"], nastyName)
	equal(t, "description", skills[0]["description"], nastyDescription)
}

func TestConcurrentSourceAdds(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const n = 6
	repos := make([]*sourceRepo, n)
	for i := range repos {
		repos[i] = h.newSourceRepo("p"+strconv.Itoa(i), true)
		repos[i].skill("one", "one", "One", nil)
		repos[i].commit("one")
	}
	outs := make([]outcome, n)
	var wg sync.WaitGroup
	for i := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i] = h.run("--json", "source", "add", repos[i].url)
		}()
	}
	wg.Wait()

	added := 0
	for i, out := range outs {
		switch out.exit {
		case 0:
			added++
		case 7: // another add held the lock, which is the documented answer
		default:
			t.Errorf("source add %s: exit %d\n%s%s", repos[i].url, out.exit, out.stdout, out.stderr)
		}
	}
	if added == 0 {
		t.Fatal("no add succeeded")
	}
	// Every remote that exists is complete, and every one has its entry.
	config := h.accountGit("config", "--list", "--local")
	entries := map[string]bool{}
	for _, raw := range readSettingsFile(t, h)["sources"].([]any) {
		entries[source.ID(raw.(map[string]any)["url"].(string))] = true
	}
	for _, r := range repos {
		id := source.ID(r.url)
		if !strings.Contains(config, "remote.src-"+id+".url") {
			continue
		}
		for _, key := range []string{"fetch", "tagopt", "promisor", "partialclonefilter"} {
			if !strings.Contains(config, "remote.src-"+id+"."+key) {
				t.Errorf("remote src-%s is missing %s:\n%s", id, key, config)
			}
		}
		if !entries[id] {
			t.Errorf("remote src-%s has no settings entry", id)
		}
	}
	equal(t, "entries", len(entries), added)
}

// gateFailedFetch holds open the blobless fetch that brings a source's
// commit and then refuses the SKILL.md batch behind it, and the
// --refetch --no-filter fallback behind that, the way failObjectFetch does.
// A run parked at the gate has written its remote and is about to lose its
// fetch, which is where a test can take the lock the take-back will need.
func gateFailedFetch(t *testing.T, h *harness) (arm func() (reached, release func())) {
	t.Helper()
	return gateGit(t, h, `sub= ; stdin= ; refetch=
for arg in "$@"; do
	case "$arg" in
	fetch) sub=fetch ;;
	--stdin) stdin=1 ;;
	--refetch) refetch=1 ;;
	esac
done
if [ "$sub" = fetch ] && { [ -n "$stdin" ] || [ -n "$refetch" ]; }; then
	if [ -n "$stdin" ]; then
		while read -r _; do :; done   # drain the object ids: PATH holds git alone, so no cat
	fi
	echo "error: Server does not allow request for unadvertised object" >&2
	exit 128
fi
[ "$sub" = fetch ] && gate=1`)
}

// sourceAddTakeBack is the log line a run writes when it starts taking its
// remote back, which it cannot write before the hold of the lock that would
// have recorded the source has failed. A test watching for it knows the
// run is past that point without sleeping for it.
func sourceAddTakeBack(id string) string { return "taking back the remote " + source.RemoteName(id) }

// TestSourceAddTakesBackItsRemoteWhenTheLockIsHeld is the invariant
// TestConcurrentSourceAdds asserts, forced instead of raced: a remote
// exists in the account repo only because a settings entry names it, or
// because an add is in flight for it. `source add` writes the remote under
// one hold of the lock and the settings entry under a second, with the
// fetch outside both; a run that loses the second hold has written a remote
// nothing will ever name again, since `source fetch` and `source skills`
// both answer from the settings. It must take the remote back, and to do
// that it must wait for the lock rather than give up on it.
//
// Nothing here is raced or retried. The gate parks the run inside one git
// of its fetch, which is after the remote is written and before the entry
// is; the lock is taken from the test while the run is parked; and it is
// held on past the point where the run said it is taking the remote back,
// which the run cannot say before the second hold has failed. Both failure
// paths into the take-back are covered: the fetch that fails and the
// settings write that cannot be made.
func TestSourceAddTakesBackItsRemoteWhenTheLockIsHeld(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		gate func(*testing.T, *harness) func() (reached, release func())
		exit int
	}{
		// The fetch fails with the remote written: the run has nothing to
		// record and must leave nothing behind.
		{"the fetch fails", gateFailedFetch, 3},
		// The fetch lands and the settings write loses the lock, which is
		// the race TestConcurrentSourceAdds runs into on a busy machine.
		{"the settings write loses the lock", gatePublish, 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			s := h.newSourceRepo("skills", true)
			s.skill("skills/alpha", "alpha", "The first skill", nil)
			s.commit("first version")
			id := source.ID(s.url)

			reached, release := tt.gate(t, h)()
			p := h.start(sourceAddTakeBack(id), "--verbose", "source", "add", s.url)
			reached() // the remote is written and the entry is not
			unlock := holdLock(t, h)
			release() // the run runs on into the hold it will lose
			p.await() // it lost it and has started taking the remote back
			// Holding the lock on past that point is what makes this test
			// discriminate at all, and it is the one duration here: an
			// acquisition that gives up is defined by a duration, so
			// nothing but elapsed time tells it from one that waits. An
			// earlier version that released the lock as soon as await
			// returned passed against a take-back that gives up, because
			// such a take-back still has 50 ms to try and an immediate
			// release hands it the lock inside them. A tenth of
			// takeBackWait is ten times that budget, so one that gives up
			// has given up before the release, and it is a tenth of the
			// bound, so one that waits still has nine tenths left. Do not
			// shorten it to make the test quicker.
			time.Sleep(takeBackWait / 10)
			unlock()

			out := p.wait()
			equal(t, "exit", out.exit, tt.exit)
			// The remote being gone is the invariant, and assertNoSource
			// below is what proves it. This says which way it was lost when
			// it is not, naming the acquisition that gave up rather than
			// leaving a reader to work back from an orphaned ref.
			if strings.Contains(out.stderr, "could not be taken back") {
				t.Errorf("the take-back gave up on the lock instead of waiting for it:\n%s", out.stderr)
			}
			assertNoSource(t, h, id, out)
		})
	}
}

// TestSourceAddReportsTheRemoteItCouldNotTakeBack covers the one case the
// bounded wait cannot answer: the lock stays held for longer than the run
// may wait. The remote is then left behind, and a remote no source names is
// the bug, so the run says so rather than exiting on the lost lock alone.
// It keeps the exit code of what stopped it – a lost lock is still 7 – and
// names the remote and the repair. The repair is then run, since a hint
// that does not work is worse than none: adding the source again rewrites
// the remote and writes the entry, which is the state this run failed to
// reach.
func TestSourceAddReportsTheRemoteItCouldNotTakeBack(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	head := s.commit("first version")
	id := source.ID(s.url)

	reached, release := gatePublish(t, h)()
	p := h.start(sourceAddTakeBack(id), "--json", "--verbose", "source", "add", s.url)
	reached()
	unlock := holdLock(t, h)
	release()
	p.await() // the settings write lost the lock; the take-back will lose it too

	out := p.wait()
	equal(t, "exit", out.exit, 7)
	e := lastError(t, h.events(out.stdout))
	equal(t, "error.code", e["code"], "locked")
	contains(t, "error.message", e["message"].(string), source.RemoteName(id)+" was left in the account repo")
	contains(t, "error.hint", e["hint"].(string), "agentx source add "+s.url)
	contains(t, "error.hint", e["hint"].(string), "agentx source remove "+id)
	contains(t, "the remote is still configured", h.accountGit("config", "--list", "--local"), "remote."+source.RemoteName(id)+".url")

	// The hint is true: adding the source again finishes the job.
	unlock()
	equal(t, "add again", h.run("source", "add", s.url).exit, 0)
	equal(t, "ref", h.accountGit("rev-parse", source.Ref(id)), head)
	settings, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	if i := settings.FindSource(s.url); i < 0 {
		t.Errorf("the repaired add wrote no settings entry: %+v", settings.Sources)
	}
}

// assertNoSource checks that nothing of a source is left on the machine:
// no remote, no ref and no settings entry. A remote without an entry is
// what `source add` may never leave behind.
func assertNoSource(t *testing.T, h *harness, id string, out outcome) {
	t.Helper()
	if config := h.accountGit("config", "--list", "--local"); strings.Contains(config, source.RemoteName(id)) {
		t.Errorf("the remote of the failed add is still configured:\n%s\nrun stderr:\n%s", config, out.stderr)
	}
	equal(t, "refs under refs/agentx", h.agentxRefs(), "")
	settings, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "settings sources", len(settings.Sources), 0)
}

// TestSourceAddKeepsTheRefWhenTheBlobsDoNotArrive is the other half of what
// a staged fetch is for. The commit reaches the account repo and its
// SKILL.md blobs never do, so the fetch fails; the source ref must still
// name the commit the last complete fetch left, or `source list` would
// report a commit no listing ever succeeded at, `source skills` would exit
// 5 and serve would drop the source from its search until the next
// successful add.
func TestSourceAddKeepsTheRefWhenTheBlobsDoNotArrive(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("skills", true) // a server that really filters
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	first := s.commit("first version")
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	id := source.ID(s.url)
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("second version")

	for _, tt := range []struct {
		name string
		stub func(*testing.T, *harness)
		exit int
	}{
		// The batch is refused and so is the full re-fetch behind it.
		{"the batch and the re-fetch fail", func(t *testing.T, h *harness) { failObjectFetch(t, h) }, 3},
		// The batch is answered and brings nothing, which no server does;
		// the blobs are checked before the ref moves, so it is caught here.
		{"the batch brings nothing", dropObjectFetch, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.stub(t, h)
			equal(t, "exit", h.run("source", "add", s.url).exit, tt.exit)

			// Nothing of the failed fetch is published, and nothing of it is
			// left behind either.
			equal(t, "ref", h.accountGit("rev-parse", source.Ref(id)), first)
			equal(t, "refs under refs/agentx", h.agentxRefs(), source.Ref(id))

			// Every reader still reads the source whole, at that commit.
			out := h.run("--json", "source", "skills", s.url)
			equal(t, "source skills exit", out.exit, 0)
			src, skills := sourceEvents(t, h.events(out.stdout))
			equal(t, "commit", src["commit"], first)
			equal(t, "skills", len(skills), 1)
			contains(t, "source list", h.run("source", "list").stdout, first[:7])
		})
	}
}

// TestSourceFetchReclaimsAStaleStagingRef covers what a fetch killed
// between its two steps leaves behind. The staging ref is the one thing a
// crash can leak, and nothing reads it. Each fetch stages on a ref of its
// own, which no later fetch can tell from the ref of one still running, so
// a killed fetch's ref stays until the source goes; the ref the configured
// refspec names, where an older agentx staged every fetch, goes with the
// next fetch as it always has. A removal takes both with the source ref,
// so nothing of a killed fetch outlives the source.
func TestSourceFetchReclaimsAStaleStagingRef(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	head := s.commit("first version")
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	id := source.ID(s.url)
	legacy := source.StagingRef(id)
	killed := source.StagingRefPrefix + "0123456789abcdef/" + id

	// What a fetch killed after its first step leaves: a ref on a commit
	// whose blobs may not be here.
	h.accountGit("update-ref", legacy, head)
	h.accountGit("update-ref", killed, head)
	out := h.run("--json", "source", "list")
	equal(t, "exit", out.exit, 0)
	contains(t, "source list", out.stdout, head)

	equal(t, "fetch", h.run("source", "fetch", s.url).exit, 0)
	equal(t, "refs after the fetch", h.agentxRefs(), killed+"\n"+source.Ref(id))

	h.accountGit("update-ref", legacy, head)
	equal(t, "remove", h.run("source", "remove", s.url).exit, 0)
	equal(t, "refs after the removal", h.agentxRefs(), "")
}
