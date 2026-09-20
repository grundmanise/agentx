package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

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
	s, _, head := h.standardSource(true)
	id := source.ID(s.url)

	out := h.run("--verbose", "source", "add", s.url)
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ added "+s.url+" at "+head[:7]+": 2 skills\n")
	equal(t, "fetches", fetches(out.stderr), 2)

	// The remote: promisor, blob:none, no tags, one refspec onto the source ref.
	for key, want := range map[string]string{
		"url":                s.url,
		"fetch":              "+HEAD:refs/agentx/sources/" + id,
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
	contains(t, "stdout", out.stdout, "✓ fetched "+s.url+" pinned to v1 at ")
	equal(t, "fetches", fetches(out.stderr), 2)
	equal(t, "refspec", h.accountGit("config", "--get", "remote.src-"+id+".fetch"), "+v1:refs/agentx/sources/"+id)
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
	fetched := events[1]["last_fetched"].(string)

	out = h.run("source", "list")
	equal(t, "exit", out.exit, 0)
	lines := strings.Split(strings.TrimSuffix(out.stdout, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "2 sources" {
		t.Fatalf("source list printed:\n%s", out.stdout)
	}
	if got, want := strings.Fields(lines[1]), []string{b.url, "(unpinned)", bHead[:7], fetched, bID}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 1 = %q, want %q", got, want)
	}
	if got, want := strings.Fields(lines[2]), []string{a.url, "v1", v1[:7], fetched, aID}; !reflect.DeepEqual(got, want) {
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
		{"empty ref", "owner/repo#", 1, "usage", "not a ref", ""},
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
	equal(t, "refspec kept", h.accountGit("config", "--get", "remote.src-"+source.ID(s.url)+".fetch"), "+HEAD:refs/agentx/sources/"+source.ID(s.url))
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
	for _, args := range [][]string{{"source", "add", s.url}, {"source", "remove", s.url}} {
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
	for _, sub := range []string{"add", "list", "skills", "remove"} {
		contains(t, "stdout", out.stdout, sub)
	}
	out = h.run("--json", "source")
	equal(t, "exit", out.exit, 1)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "usage")
	out = h.run("--json", "source", "add")
	equal(t, "exit", out.exit, 1)
}
