package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// None of these tests is parallel: each swaps cliVersion, thisExecutable
// and the release URLs for the length of a run.

// release is one release a fake GitHub serves: its version and the
// archives it holds, by asset name. A nightly is told by its version.
type release struct {
	version string
	assets  map[string][]byte
}

// fakeGitHub serves releases the way agentx update reads them from
// github.com and api.github.com: the redirect of releases/latest to the
// newest release that is no nightly, the release list of the API, newest
// first, and the download of each asset and of checksums.txt. It points
// the run at itself until the test ends.
func fakeGitHub(t *testing.T, releases ...release) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+releaseRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		for _, rel := range releases {
			if !isNightly(rel.version) {
				http.Redirect(w, r, "/"+releaseRepo+"/releases/tag/v"+rel.version, http.StatusFound)
				return
			}
		}
		http.Redirect(w, r, "/"+releaseRepo+"/releases", http.StatusFound)
	})
	mux.HandleFunc("/repos/"+releaseRepo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "a User-Agent is required", http.StatusForbidden)
			return
		}
		var list []map[string]any
		for _, rel := range releases {
			list = append(list, map[string]any{"tag_name": "v" + rel.version, "prerelease": isNightly(rel.version)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("/"+releaseRepo+"/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/"+releaseRepo+"/releases/download/")
		tag, name, _ := strings.Cut(rest, "/")
		for _, rel := range releases {
			if "v"+rel.version != tag {
				continue
			}
			if name == "checksums.txt" {
				if b, ok := rel.assets[name]; ok { // a release whose checksums the test wrote
					_, _ = w.Write(b)
					return
				}
				var sums strings.Builder
				for asset, b := range rel.assets {
					sum := sha256.Sum256(b)
					fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
				}
				_, _ = w.Write([]byte(sums.String()))
				return
			}
			if b, ok := rel.assets[name]; ok {
				_, _ = w.Write(b)
				return
			}
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	site, api := releaseSite, releaseAPI
	releaseSite, releaseAPI = srv.URL, srv.URL
	t.Cleanup(func() { releaseSite, releaseAPI = site, api })
	return srv
}

// thisPlatform is the asset name of the archive built for the machine the
// tests run on.
var thisPlatform = "agentx_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"

// archive is a release archive as the release workflow writes it, whose
// agentx is a script reporting version, so that a test replaces a file
// with something that runs.
func archive(t *testing.T, version string) []byte {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '{\"type\":\"version\",\"schema_version\":%d,\"cli_version\":\"%s\"}'\n", schemaVersion, version)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
		body string
	}{{"agentx", 0o755, script}, {"LICENSE", 0o644, "Apache-2.0\n"}, {"README.md", 0o644, "# agentx\n"}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// released is a release of version with an archive for this machine.
func released(t *testing.T, version string) release {
	t.Helper()
	return release{version: version, assets: map[string][]byte{thisPlatform: archive(t, version)}}
}

// installedCLI makes the run believe it is agentx version, running from a
// file in h's home that holds content, and returns the file's path.
func installedCLI(t *testing.T, h *harness, version, content string) string {
	t.Helper()
	dir := filepath.Join(h.home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agentx")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	swapUpdate(t, version, path)
	return path
}

// swapUpdate makes the run report version and run from path until the
// test ends.
func swapUpdate(t *testing.T, version, path string) {
	t.Helper()
	was, exe := cliVersion, thisExecutable
	cliVersion = version
	thisExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { cliVersion, thisExecutable = was, exe })
}

func TestUpdateReplacesTheFileWithTheLatestRelease(t *testing.T) {
	h := newHarness(t)
	fakeGitHub(t, released(t, "0.4.0"), released(t, "0.4.0-nightly.20261010"), released(t, "0.3.0"))
	path := installedCLI(t, h, "0.3.0", "the old agentx\n")
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}

	out := h.run("update", "--json")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"update", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "update.state", events[0]["state"], "updated")
	equal(t, "update.channel", events[0]["channel"], "stable")
	equal(t, "update.current_version", events[0]["current_version"], "0.3.0")
	equal(t, "update.version", events[0]["version"], "0.4.0")
	equal(t, "update.path", events[0]["path"], path)
	equal(t, "result.summary", events[1]["summary"], "updated agentx 0.3.0 to 0.4.0 at "+path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "the file", string(got), `"cli_version":"0.4.0"`)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "mode", info.Mode().Perm(), os.FileMode(0o700))
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "files beside it", len(entries), 1)
}

func TestUpdateCheckReportsWhatTheChannelOffers(t *testing.T) {
	h := newHarness(t)
	fakeGitHub(t, released(t, "0.4.0"), released(t, "0.4.0-nightly.20261010"), released(t, "0.3.0"))
	path := installedCLI(t, h, "0.3.0", "the old agentx\n")

	out := h.run("update", "--check")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "• agentx 0.4.0 is available, you have 0.3.0: run 'agentx update' to install it\n")
	equal(t, "stderr", out.stderr, "")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the file", string(got), "the old agentx\n")

	// A release follows the releases and a nightly the nightlies; the flags
	// move between them, and --version names one.
	for _, c := range []struct {
		version string
		args    []string
		want    string
	}{
		{"0.4.0", nil, "✓ agentx 0.4.0 is the latest release"},
		{"0.4.0", []string{"--nightly"}, "• agentx 0.4.0-nightly.20261010 is available, you have 0.4.0: run 'agentx update --nightly' to install it"},
		{"0.4.0-nightly.20261001", nil, "• agentx 0.4.0-nightly.20261010 is available, you have 0.4.0-nightly.20261001: run 'agentx update' to install it"},
		{"0.4.0-nightly.20261010", nil, "✓ agentx 0.4.0-nightly.20261010 is the newest nightly"},
		{"0.4.0-nightly.20261010", []string{"--stable"}, "• agentx 0.4.0 is available, you have 0.4.0-nightly.20261010: run 'agentx update --stable' to install it"},
		{"0.4.0", []string{"--version", "v0.3.0"}, "• agentx 0.3.0 is available, you have 0.4.0: run 'agentx update --version 0.3.0' to install it"},
		{"0.3.0", []string{"--version", "0.3.0"}, "✓ agentx is already 0.3.0"},
	} {
		swapUpdate(t, c.version, path)
		out := h.run(append([]string{"update", "--check"}, c.args...)...)
		equal(t, fmt.Sprintf("%s %v exit", c.version, c.args), out.exit, 0)
		equal(t, fmt.Sprintf("%s %v stdout", c.version, c.args), out.stdout, c.want+"\n")
	}
}

func TestUpdateRefusals(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		name    string
		version string // this agentx's, 0.3.0 when empty
		setup   func(t *testing.T, path string)
		args    []string
		exit    int
		message string
	}{
		{name: "a build from source", version: "dev", exit: 6,
			message: "this agentx is a build from source, which no release can replace"},
		{name: "Homebrew's agentx", exit: 6,
			setup: func(t *testing.T, path string) {
				swapUpdate(t, "0.3.0", "/opt/homebrew/Cellar/agentx/0.3.0/bin/agentx")
			},
			message: "agentx at /opt/homebrew/Cellar/agentx/0.3.0/bin/agentx was installed by Homebrew"},
		{name: "the app's agentx", exit: 6,
			setup: func(t *testing.T, path string) {
				swapUpdate(t, "0.3.0", "/Applications/agentx.app/Contents/MacOS/agentx")
			},
			message: "agentx at /Applications/agentx.app/Contents/MacOS/agentx is the one inside the agentx app, which updates it"},
		{name: "the flags combined", args: []string{"--nightly", "--version", "0.4.0"}, exit: 1,
			message: "--nightly, --stable and --version cannot be combined"},
		{name: "a version with no release", args: []string{"--version", "9.9.9"}, exit: 5,
			message: "release v9.9.9 has no " + thisPlatform},
		{name: "an archive that does not match its checksum", exit: 6,
			setup: func(t *testing.T, path string) {
				fakeGitHub(t, release{version: "0.4.0", assets: map[string][]byte{
					thisPlatform:    archive(t, "0.4.0"),
					"checksums.txt": []byte(strings.Repeat("0", 64) + "  " + thisPlatform + "\n"),
				}})
			},
			message: thisPlatform + " does not match its checksum"},
		{name: "a download that reports another version", exit: 6,
			setup: func(t *testing.T, path string) {
				fakeGitHub(t, release{version: "0.4.0", assets: map[string][]byte{thisPlatform: archive(t, "0.3.9")}})
			},
			message: "the downloaded agentx does not report version 0.4.0"},
		{name: "GitHub out of reach", exit: 3,
			setup: func(t *testing.T, path string) {
				fakeGitHub(t).Close()
			},
			message: "cannot reach "},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeGitHub(t, released(t, "0.4.0"), released(t, "0.3.0"))
			version := c.version
			if version == "" {
				version = "0.3.0"
			}
			path := installedCLI(t, h, version, "the old agentx\n")
			if c.setup != nil {
				c.setup(t, path)
			}
			out := h.run(append([]string{"update"}, c.args...)...)
			equal(t, "exit", out.exit, c.exit)
			contains(t, "stderr", out.stderr, "error: "+c.message)
			contains(t, "stderr", out.stderr, "hint: ")
			equal(t, "stdout", out.stdout, "")
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, "the file", string(got), "the old agentx\n")
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, "files beside it", len(entries), 1)
		})
	}
}
