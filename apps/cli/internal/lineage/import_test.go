package lineage

import (
	"errors"
	"strings"
	"testing"
)

const (
	commitID = "0123456789abcdef0123456789abcdef01234567"
	hashID   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// message is an import commit message with the four trailers, each
// replaceable, so that one case changes one thing.
func message(source, path, commit, hash string) string {
	return "import x at y content z\n\n" +
		TrailerSource + ": " + source + "\n" +
		TrailerPath + ": " + path + "\n" +
		TrailerCommit + ": " + commit + "\n" +
		TrailerHash + ": " + hash + "\n"
}

func TestParseReadsTheFourTrailers(t *testing.T) {
	t.Parallel()
	imported, err := Parse(message("https://github.com/example/skills", "skills/pdf", commitID, hashID))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range []struct{ what, got, want string }{
		{"source", imported.Source, "https://github.com/example/skills"},
		{"path", imported.Path, "skills/pdf"},
		{"commit", imported.Commit, commitID},
		{"hash", imported.Hash, hashID},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	// A skill at the repository root has "." as its path trailer, which
	// reads back as the empty subpath: a trailer with no value would be one
	// a reader cannot tell from a missing one.
	root, err := Parse(message("https://github.com/example/skills", ".", commitID, hashID))
	if err != nil || root.Path != "" {
		t.Errorf("the root path trailer read back as %q: %v", root.Path, err)
	}
}

func TestParseRefusesAnythingElse(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, message string }{
		{"a plugin coordinate as the source", message("docx@anthropics", "skills/pdf", commitID, hashID)},
		{"a plugin coordinate with a marketplace path", message("anthropics/docx@marketplace", "skills/pdf", commitID, hashID)},
		{"a source URL that is not canonical", message("https://GitHub.com/example/skills.git", "skills/pdf", commitID, hashID)},
		{"a source URL carrying a subpath", message("https://github.com/example/skills/tree/main/pdf", "skills/pdf", commitID, hashID)},
		{"no source", strings.ReplaceAll(message("https://github.com/example/skills", "skills/pdf", commitID, hashID), TrailerSource+": https://github.com/example/skills\n", "")},
		{"no path", strings.ReplaceAll(message("https://github.com/example/skills", "skills/pdf", commitID, hashID), TrailerPath+": skills/pdf\n", "")},
		{"a path that leaves the repository", message("https://github.com/example/skills", "../elsewhere", commitID, hashID)},
		{"an absolute path", message("https://github.com/example/skills", "/etc", commitID, hashID)},
		{"an unclean path", message("https://github.com/example/skills", "skills//pdf", commitID, hashID)},
		{"a short commit", message("https://github.com/example/skills", "skills/pdf", commitID[:7], hashID)},
		{"a commit that is not hex", message("https://github.com/example/skills", "skills/pdf", strings.Repeat("z", 40), hashID)},
		{"a hash of the wrong length", message("https://github.com/example/skills", "skills/pdf", commitID, hashID[:40])},
		{"a trailer given twice", message("https://github.com/example/skills", "skills/pdf", commitID, hashID) + TrailerHash + ": " + hashID + "\n"},
		{"a message with no trailers at all", "import something\n"},
		{"the machine trailer instead of a source", "import x\n\nAgentx-Machine: 0123456789abcdef0123456789abcdef\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.message); !errors.Is(err, ErrTrailer) {
				t.Errorf("parse accepted %s: %v", c.name, err)
			}
		})
	}
}

// TestMessageRoundTrips checks that what Write puts in a commit is what
// Parse reads out of it, the root subpath included.
func TestMessageRoundTrips(t *testing.T) {
	t.Parallel()
	for _, subpath := range []string{"", "skills/pdf", "a/b/c"} {
		want := Import{Source: "https://github.com/example/skills", Path: subpath, Commit: commitID, Hash: hashID}
		got, err := Parse(want.Message())
		if err != nil {
			t.Fatalf("parse %q: %v", subpath, err)
		}
		if got != want {
			t.Errorf("round trip of %q = %#v, want %#v", subpath, got, want)
		}
	}
	// The subject names the four things the commit depends on and nothing
	// else: no library name, which the frontmatter decides, and no machine.
	subject, _, _ := strings.Cut(Import{Source: "https://github.com/example/skills", Path: "skills/pdf", Commit: commitID, Hash: hashID}.Message(), "\n")
	want := "import https://github.com/example/skills/skills/pdf at " + commitID[:12] + " content " + hashID[:12]
	if subject != want {
		t.Errorf("subject = %q, want %q", subject, want)
	}
}

func TestUpstreamDateIsEpochWithoutAnOffset(t *testing.T) {
	t.Parallel()
	when, err := UpstreamDate("1700000000")
	if err != nil || when != "1700000000 +0000" {
		t.Errorf("UpstreamDate = %q, %v", when, err)
	}
	for _, bad := range []string{"", "  ", "2026-09-21", "1700000000 +0300", "-1"} {
		if _, err := UpstreamDate(bad); err == nil {
			t.Errorf("UpstreamDate accepted %q", bad)
		}
	}
	// An epoch beyond what git stores is refused rather than carried into a
	// commit. fast-import takes any run of digits, so nothing downstream
	// would refuse it, and the commit would be dated where no reader can
	// put it; commit-tree, which writes the same commit for everything the
	// two both accept, refuses it outright.
	for _, wide := range []string{"9223372036854775808", strings.Repeat("9", 40)} {
		if _, err := UpstreamDate(wide); err == nil {
			t.Errorf("UpstreamDate accepted %q, which is not a time git stores", wide)
		}
	}
	// What git does write is taken, to the second before the overflow and
	// past the 32 bits commit-tree stops at.
	for _, ok := range []string{"0", "4294967296", "100000000000", "9223372036854775807"} {
		if _, err := UpstreamDate(ok); err != nil {
			t.Errorf("UpstreamDate refused %q: %v", ok, err)
		}
	}
}

// TestParseReadsTheTrailerBlockAlone keeps Parse to the last paragraph of a
// message, which is where git keeps trailers. A fork's tip is read for
// lineage on every listing while its message is whatever the user wrote, so
// a body that quotes the four trailers must not be read as lineage: it
// would credit the fork with a version it does not hold.
func TestParseReadsTheTrailerBlockAlone(t *testing.T) {
	t.Parallel()
	trailers := "Agentx-Source: https://github.com/example/skills\n" +
		TrailerPath + ": skills/pdf\n" +
		TrailerCommit + ": " + commitID + "\n" +
		TrailerHash + ": " + hashID + "\n"
	for _, c := range []struct{ name, message string }{
		{"the four trailers quoted in the body", "merge upstream\n\n" + trailers + "\nand then I changed the prompt by hand\n"},
		{"one trailer quoted in the body", "merge upstream\n\nAgentx-Source: https://github.com/example/skills\n\nnotes about the merge\n"},
		{"the trailers as the only paragraph", trailers},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.message); !errors.Is(err, ErrTrailer) {
				t.Errorf("parse read %s as lineage: %v", c.name, err)
			}
		})
	}
	// The message an import commit actually carries still reads back.
	if _, err := Parse("import x at y content z\n\n" + trailers); err != nil {
		t.Errorf("parse refused an import commit's own message: %v", err)
	}
}

// TestParseRefusesAFifthTrailer is the deliberate reading of "there are
// four trailers and no more": an extra Agentx- trailer is refused rather
// than ignored, since a message carrying one was not written as an import
// commit and reading it as one would credit a version to a commit that does
// not hold it. A trailer that is not agentx's at all is another matter and
// is ignored, as git's own readers ignore what is not theirs.
func TestParseRefusesAFifthTrailer(t *testing.T) {
	t.Parallel()
	four := message("https://github.com/example/skills", "skills/pdf", commitID, hashID)
	for _, extra := range []string{
		"Agentx-Machine: 0123456789abcdef0123456789abcdef",
		"Agentx-Fork: mine",
		"Agentx-Account: someone",
	} {
		if _, err := Parse(four + extra + "\n"); !errors.Is(err, ErrTrailer) {
			t.Errorf("parse accepted the extra trailer %q: %v", extra, err)
		}
	}
	// A trailer of somebody else's in the same block is not agentx's to
	// refuse: the four are all there and they decide the commit.
	if _, err := Parse(four + "Signed-off-by: Someone <someone@example.com>\n"); err != nil {
		t.Errorf("parse refused a message carrying a trailer that is not agentx's: %v", err)
	}
}

// TestValidPath is what may be a subpath of a source, which is both the
// path an import commit records and the path another tool's lock file
// names. A path that is not a directory of the repository, or that could
// not be written on one line of a commit message or read back from one line
// of git's batch input, is none.
func TestValidPath(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path string
		want bool
	}{
		{"", true},
		{"skills/alpha", true},
		{"a", true},
		{".", false},
		{"..", false},
		{"../etc", false},
		{"/etc", false},
		{"./skills", false},
		{"skills/", false},
		{"skills//alpha", false},
		{"skills/../../etc", false},
		{"skills/al\npha", false},
		{"skills/al\tpha", false},
		{"skills/al\x00pha", false},
		// A directory a source may perfectly well hold, and a value no
		// trailer can carry back: the reader trims the whitespace around a
		// trailer's value, so the line these were written on would be read
		// as another directory of the same source.
		{" skills", false},
		{"skills ", false},
		{"skills/alpha\u00a0", false},
		{"skills/al pha", true},
	} {
		if got := ValidPath(c.path); got != c.want {
			t.Errorf("ValidPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestUnrecordableIsParseItself holds the write side of an import commit to
// the read side: what agentx would record has to be what a reader reads
// back, coordinate for coordinate. A version whose lineage fails this
// installs with no lineage at all, or with lineage naming another
// directory, and says nothing about it either way.
func TestUnrecordableIsParseItself(t *testing.T) {
	t.Parallel()
	const url = "https://github.com/example/skills"
	for _, c := range []struct {
		name string
		imp  Import
		want bool // the lineage is recordable
	}{
		{"a version of a subdirectory", Import{Source: url, Path: "skills/pdf", Commit: commitID, Hash: hashID}, true},
		{"a version of the repository root", Import{Source: url, Path: "", Commit: commitID, Hash: hashID}, true},
		{"a directory carrying an escape", Import{Source: url, Path: "na\x1b[31msty", Commit: commitID, Hash: hashID}, false},
		{"a directory carrying a newline", Import{Source: url, Path: "tools/od\nd", Commit: commitID, Hash: hashID}, false},
		{"a directory opening with a space", Import{Source: url, Path: " pdf", Commit: commitID, Hash: hashID}, false},
		{"a directory closing with a space", Import{Source: url, Path: "pdf ", Commit: commitID, Hash: hashID}, false},
		{"a directory spelled as the root is", Import{Source: url, Path: ".", Commit: commitID, Hash: hashID}, false},
		{"a source that is not canonical", Import{Source: url + ".git", Path: "pdf", Commit: commitID, Hash: hashID}, false},
		{"a source carrying a subpath", Import{Source: url + "/tree/main/pdf", Path: "pdf", Commit: commitID, Hash: hashID}, false},
		{"an upstream commit that is abbreviated", Import{Source: url, Path: "pdf", Commit: commitID[:12], Hash: hashID}, false},
		{"a content hash that is not one", Import{Source: url, Path: "pdf", Commit: commitID, Hash: "not a hash"}, false},
		{"no content hash at all", Import{Source: url, Path: "pdf", Commit: commitID}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			why := Unrecordable(c.imp)
			if (why == "") != c.want {
				t.Fatalf("Unrecordable(%#v) = %q, want recordable %v", c.imp, why, c.want)
			}
			if !c.want {
				// The reason is the reader's own, so it names the trailer
				// at fault and never repeats an escape a terminal obeys.
				if !strings.Contains(why, "Agentx-") {
					t.Errorf("the reason %q names no trailer", why)
				}
				if strings.ContainsAny(why, "\x1b\n\t") {
					t.Errorf("the reason %q carries a control character", why)
				}
				return
			}
			got, err := Parse(c.imp.Message())
			if err != nil || got != c.imp {
				t.Errorf("recordable lineage read back as %#v: %v", got, err)
			}
		})
	}
}

// TestUnrecordableRefusesWhatParseReadsBackAsAnother is the case a
// predicate over one coordinate cannot answer: the trailers are written
// and read back whole, so what is refused is a value no line of a message
// carries, not a value the coordinate itself is wrong in. A subpath padded
// with a space is a directory git holds and ValidPath, given the trimmed
// value a reader hands it, would accept.
func TestUnrecordableRefusesWhatParseReadsBackAsAnother(t *testing.T) {
	t.Parallel()
	padded := Import{Source: "https://github.com/example/skills", Path: "pdf ", Commit: commitID, Hash: hashID}
	if !ValidPath(strings.TrimSpace(padded.Path)) {
		t.Fatal("the trimmed path is not the one a reader would accept")
	}
	read, err := Parse(padded.Message())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if read.Path == padded.Path {
		t.Fatal("the padded path read back unchanged")
	}
	if why := Unrecordable(padded); why == "" {
		t.Fatal("a padded subpath is recordable, so an install would write lineage naming another directory")
	}
}
