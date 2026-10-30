package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// releaseRepo is the GitHub repository whose releases carry the CLI: the
// same archives scripts/install.sh installs, verified by the same
// checksums.txt (ADR 0007).
const releaseRepo = "grundmanise/agentx"

// Where the releases are read from, and which file this agentx is. A test
// points them at a server and a file of its own.
var (
	releaseSite    = "https://github.com"
	releaseAPI     = "https://api.github.com"
	thisExecutable = os.Executable
)

// The channel a version belongs to: a nightly is vX.Y.Z-nightly.YYYYMMDD,
// anything else is a release.
const (
	channelStable  = "stable"
	channelNightly = "nightly"
)

// The states an update event reports.
const (
	cliCurrent   = "current"   // this agentx is the version the channel offers
	cliAvailable = "available" // another version is offered; --check stops here
	cliUpdated   = "updated"   // the file was replaced
)

// The bounds of what is read from a release: the archives are a few
// megabytes, and checksums.txt a few lines.
const (
	maxArchiveBytes   = 256 << 20
	maxChecksumsBytes = 1 << 20
	maxListingBytes   = 8 << 20
)

// versionCheckBudget is how long the downloaded agentx gets to report its
// version before it is judged broken.
const versionCheckBudget = 10 * time.Second

type updateEvent struct {
	event
	State          string `json:"state"`
	Channel        string `json:"channel"`
	CurrentVersion string `json:"current_version"`
	Version        string `json:"version"`
	Path           string `json:"path"`
}

// updateSelection is what one run of agentx update was asked to do.
type updateSelection struct {
	check   bool
	nightly bool
	stable  bool
	version string
}

// hint is the command line that applies the update the run was asked
// about, for the line --check ends with.
func (s updateSelection) hint() string {
	cmd := "agentx update"
	switch {
	case s.nightly:
		cmd += " --nightly"
	case s.stable:
		cmd += " --stable"
	case s.version != "":
		cmd += " --version " + s.version
	}
	return cmd
}

func newUpdateCommand(inv *invocation) *cobra.Command {
	var sel updateSelection
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update agentx to the latest release, or check for one",
		Long: `Update agentx to the latest release, or check for one.

Replaces the agentx that runs this command with the release for this system, downloaded from
https://github.com/` + releaseRepo + `/releases and checked against the release's checksums. A nightly
build follows the nightlies and a release follows the releases; --nightly and --stable move
between the two, and --version installs one version.

An agentx that Homebrew installed is updated with 'brew upgrade agentx', and the one inside the
agentx desktop app is updated with the app: both are refused here. A build from source has no
release to take: build it again.`,
		Annotations: map[string]string{annotationSection: "diagnostics"},
		Args:        cobra.NoArgs,
		Example: examples(
			"agentx update", "replace this agentx with the latest release",
			"agentx update --check", "say whether a newer version is out; nothing is written",
			"agentx update --nightly", "move to the newest nightly build",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			given := 0
			for _, on := range []bool{sel.nightly, sel.stable, sel.version != ""} {
				if on {
					given++
				}
			}
			if given > 1 {
				return fail(exitUsage, "--nightly, --stable and --version cannot be combined", "choose one of them")
			}
			sel.version = strings.TrimPrefix(sel.version, "v")
			return (&updater{inv: inv, sel: sel}).run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&sel.check, "check", false, "report whether another version is offered and change nothing")
	cmd.Flags().BoolVar(&sel.nightly, "nightly", false, "move to the newest nightly build")
	cmd.Flags().BoolVar(&sel.stable, "stable", false, "move to the latest release, from a nightly")
	cmd.Flags().StringVar(&sel.version, "version", "", "install this version, such as 0.2.0")
	return cmd
}

// isNightly reports whether version is a nightly build's.
func isNightly(version string) bool { return strings.Contains(version, "-nightly.") }

// channelOf is the channel a version belongs to.
func channelOf(version string) string {
	if isNightly(version) {
		return channelNightly
	}
	return channelStable
}

// updater is one run of agentx update.
type updater struct {
	inv *invocation
	sel updateSelection
}

func (u *updater) run(ctx context.Context) error {
	out := u.inv.out
	current := cliVersion
	if current == "dev" {
		return fail(exitRefused, "this agentx is a build from source, which no release can replace",
			"build it again from source, or install a release: curl -fsSL https://agentx.wtf/install | sh")
	}
	path, err := thisExecutable()
	if err != nil {
		return fail(exitInternal, "cannot tell which file agentx runs from: "+err.Error(), "install again with the install script: curl -fsSL https://agentx.wtf/install | sh")
	}
	// The file itself, where agentx is run through a symlink; a path that
	// cannot be resolved is left as it is, for the replacement to report.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if !u.sel.check {
		if f := managedElsewhere(path); f != nil {
			return f
		}
	}

	// The version the run is about: the one asked for, or the newest of the
	// channel this agentx is on or was moved to.
	var target string
	switch {
	case u.sel.version != "":
		target = u.sel.version
	case u.sel.nightly || (!u.sel.stable && isNightly(current)):
		target, err = u.newestNightly(ctx)
	default:
		target, err = u.latestRelease(ctx)
	}
	if err != nil {
		return err
	}
	channel := channelOf(target)
	report := func(state, text string) {
		out.emit(updateEvent{event: newEvent("update"), State: state, Channel: channel, CurrentVersion: current, Version: target, Path: path})
		u.inv.summary = text
	}
	if target == current {
		text := "agentx is already " + target
		switch {
		case u.sel.version != "":
		case channel == channelNightly:
			text = "agentx " + target + " is the newest nightly"
		default:
			text = "agentx " + target + " is the latest release"
		}
		report(cliCurrent, text)
		out.print(out.paint(okStyle, glyphOK), " ", text)
		return nil
	}
	if u.sel.check {
		text := fmt.Sprintf("agentx %s is available, you have %s: run '%s' to install it", target, current, u.sel.hint())
		report(cliAvailable, text)
		out.print(out.paint(infoStyle, glyphInfo), " ", text)
		return nil
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	out.debugf("downloading agentx %s for %s from %s", target, platform, releaseSite)
	binary, err := u.download(ctx, target, "agentx_"+platform+".tar.gz")
	if err != nil {
		return err
	}
	if err := replaceExecutable(ctx, path, binary, target); err != nil {
		return err
	}
	text := fmt.Sprintf("updated agentx %s to %s at %s", current, target, quotedPath(path))
	report(cliUpdated, text)
	out.done(text)
	return nil
}

// managedElsewhere is the refusal for an agentx another tool owns, which
// that tool updates: Homebrew's, in its Cellar, and the desktop app's,
// inside its bundle, which is the CLI of the app's own release. Nil for a
// file agentx may replace.
func managedElsewhere(path string) error {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		switch {
		case part == "Cellar":
			return fail(exitRefused, "agentx at "+quotedPath(path)+" was installed by Homebrew", "run 'brew upgrade agentx'")
		case strings.HasSuffix(part, ".app") && i+1 < len(parts) && parts[i+1] == "Contents":
			return fail(exitRefused, "agentx at "+quotedPath(path)+" is the one inside the agentx app, which updates it",
				"download the newer app from https://github.com/"+releaseRepo+"/releases")
		}
	}
	return nil
}

// releasesURL is the page every hint sends the reader to.
const releasesURL = "https://github.com/" + releaseRepo + "/releases"

// get fetches url with the headers GitHub wants. The API is read with the
// GITHUB_TOKEN of the environment when there is one, since GitHub allows an
// anonymous client 60 requests an hour. A redirect is followed unless
// stay says to answer with it.
func (u *updater) get(ctx context.Context, rawURL, accept string, stay bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fail(exitInternal, err.Error(), "")
	}
	req.Header.Set("User-Agent", "agentx/"+cliVersion)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token := u.inv.env["GITHUB_TOKEN"]; token != "" && strings.HasPrefix(rawURL, releaseAPI) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{}
	if stay {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	resp, err := client.Do(req)
	if err != nil {
		host := req.URL.Host
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fail(exitSource, fmt.Sprintf("cannot reach %s: %v", host, err), "check the network connection and try again")
	}
	return resp, nil
}

// readAll reads a response whole, up to limit bytes, and closes it.
func readAll(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fail(exitSource, fmt.Sprintf("the download from %s was cut short: %v", resp.Request.URL.Host, err), "try again")
	}
	if int64(len(b)) > limit {
		return nil, fail(exitRefused, fmt.Sprintf("%s is larger than %d bytes", resp.Request.URL, limit), "see "+releasesURL)
	}
	return b, nil
}

// unexpected is the failure for a status GitHub answered that the run
// cannot use.
func unexpected(resp *http.Response) error {
	hint := "try again later"
	if resp.StatusCode == http.StatusForbidden && strings.HasPrefix(resp.Request.URL.String(), releaseAPI) {
		hint = "GitHub allows 60 requests an hour without GITHUB_TOKEN; set it and try again"
	}
	return fail(exitSource, fmt.Sprintf("GitHub answered %s for %s", resp.Status, resp.Request.URL), hint)
}

// latestRelease is the version of the newest release that is not a
// prerelease, which GitHub redirects releases/latest to, so that no API
// request is spent on it.
func (u *updater) latestRelease(ctx context.Context) (string, error) {
	resp, err := u.get(ctx, releaseSite+"/"+releaseRepo+"/releases/latest", "", true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", unexpected(resp)
	}
	location := resp.Header.Get("Location")
	if _, tag, ok := strings.Cut(location, "/releases/tag/"); ok && tag != "" {
		return strings.TrimPrefix(tag, "v"), nil
	}
	return "", fail(exitNotFound, "agentx has no release yet", "run 'agentx update --nightly' for the newest nightly build")
}

// newestNightly is the version of the newest nightly prerelease: GitHub
// lists releases newest first, and a nightly is tagged vX.Y.Z-nightly.YYYYMMDD.
func (u *updater) newestNightly(ctx context.Context) (string, error) {
	resp, err := u.get(ctx, releaseAPI+"/repos/"+releaseRepo+"/releases?per_page=50", "application/vnd.github+json", false)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return "", unexpected(resp)
	}
	b, err := readAll(resp, maxListingBytes)
	if err != nil {
		return "", err
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &releases); err != nil {
		return "", fail(exitSource, "cannot read the release list GitHub sent: "+err.Error(), "try again later")
	}
	for _, r := range releases {
		if isNightly(r.TagName) {
			return strings.TrimPrefix(r.TagName, "v"), nil
		}
	}
	return "", fail(exitNotFound, "there is no nightly release yet", "see "+releasesURL)
}

// download fetches the archive asset of release version, checks it against
// the release's checksums.txt and returns the agentx binary it holds.
func (u *updater) download(ctx context.Context, version, asset string) ([]byte, error) {
	base := releaseSite + "/" + releaseRepo + "/releases/download/v" + version + "/"
	fetch := func(name string, limit int64) ([]byte, error) {
		resp, err := u.get(ctx, base+name, "", false)
		if err != nil {
			return nil, err
		}
		switch resp.StatusCode {
		case http.StatusOK:
			return readAll(resp, limit)
		case http.StatusNotFound:
			resp.Body.Close()
			return nil, fail(exitNotFound, fmt.Sprintf("release v%s has no %s", version, name), "see "+releasesURL)
		}
		resp.Body.Close()
		return nil, unexpected(resp)
	}
	archive, err := fetch(asset, maxArchiveBytes)
	if err != nil {
		return nil, err
	}
	checksums, err := fetch("checksums.txt", maxChecksumsBytes)
	if err != nil {
		return nil, err
	}
	if err := verifyChecksum(archive, asset, checksums); err != nil {
		return nil, err
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return nil, fail(exitRefused, fmt.Sprintf("%s of release v%s is not an archive agentx can read: %v", asset, version, err), "see "+releasesURL)
	}
	return binary, nil
}

// verifyChecksum checks archive against the line of checksums.txt that
// names asset, as sha256sum writes it: the hash, spaces, the file name.
func verifyChecksum(archive []byte, asset string, checksums []byte) error {
	var expected string
	for line := range strings.Lines(string(checksums)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			expected = fields[0]
		}
	}
	if expected == "" {
		return fail(exitRefused, "checksums.txt lists no "+asset, "see "+releasesURL)
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != expected {
		return fail(exitRefused, asset+" does not match its checksum", "try again")
	}
	return nil
}

// extractBinary is the agentx file of a release archive: a gzipped tar
// holding agentx, LICENSE and README.md.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("it holds no agentx file")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "agentx" {
			b, err := io.ReadAll(io.LimitReader(tr, maxArchiveBytes))
			if err != nil {
				return nil, err
			}
			return b, nil
		}
	}
}

// replaceExecutable puts binary in place of the file at path, with the
// mode the file had: written beside it, run once to check that it is an
// agentx reporting version, then renamed over the file, which replaces it
// in one step while agentx runs. Whatever stops the replacement leaves the
// file as it was.
func replaceExecutable(ctx context.Context, path string, binary []byte, version string) error {
	cannot := func(err error) error {
		return fail(exitRefused, fmt.Sprintf("cannot replace %s: %v", quotedPath(path), err),
			"run agentx update as the user who installed agentx, or install again with the install script: curl -fsSL https://agentx.wtf/install | sh")
	}
	info, err := os.Stat(path)
	if err != nil {
		return cannot(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentx.*")
	if err != nil {
		return cannot(err)
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return cannot(err)
	}
	if err := tmp.Chmod(info.Mode().Perm() | 0o100); err != nil {
		tmp.Close()
		return cannot(err)
	}
	if err := tmp.Close(); err != nil {
		return cannot(err)
	}
	if err := checkVersion(ctx, tmp.Name(), version); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return cannot(err)
	}
	keep = true
	return nil
}

// checkVersion runs the downloaded agentx at path and checks that it
// reports version, before it replaces anything.
func checkVersion(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, versionCheckBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fail(exitRefused, fmt.Sprintf("the downloaded agentx %s does not run: %s", version, detail), "see "+releasesURL)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf(`"cli_version":%q`, version)) {
		return fail(exitRefused, fmt.Sprintf("the downloaded agentx does not report version %s", version), "see "+releasesURL)
	}
	return nil
}
