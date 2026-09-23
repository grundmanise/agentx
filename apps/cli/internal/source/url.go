// Package source parses source URLs and keeps sources in the account repo:
// one named remote and one ref per source, fetched without blobs, plus the
// SKILL.md blobs of the skills inside.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Source is one parsed source URL: the canonical URL that identifies the
// source, the subpath a listing is scoped to and the ref the source is
// pinned to. The URL never carries a user or a token.
type Source struct {
	URL      string // the canonical clone URL
	Subpath  string // a directory inside the repository, "" for the whole of it
	Ref      string // the pinned branch, tag or commit, "" for the remote's default branch
	Stripped bool   // a user or token was removed from the input
}

// ID is the source's id: the first 16 hex characters of the SHA-256 of its
// canonical URL. It names the remote and the ref in the account repo.
func (s Source) ID() string { return ID(s.URL) }

// ID derives the source id of a canonical URL.
func ID(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:16]
}

// IsID reports whether s has the shape of a source id.
func IsID(s string) bool { return idPattern.MatchString(s) }

var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// The forms Parse accepts, for the hint of a usage error.
const Forms = "owner/repo, owner/repo/subpath, a GitHub or GitLab URL with an optional tree path, an SSH URL or a file:// URL, each with an optional #ref"

// ErrForm is the error of an input that is not a source URL.
var ErrForm = errors.New("not a source URL")

// scpLike is user@host:path, the SSH shorthand git accepts.
var scpLike = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9._-]+):(.*)$`)

// shorthand is owner/repo with an optional subpath, resolved to GitHub.
var shorthand = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+(/.*)?$`)

// The first path segment after owner/repo on GitHub that is not a subpath
// but a page of the web site.
var githubPages = map[string]bool{
	"blob": true, "commit": true, "commits": true, "pull": true, "pulls": true,
	"issues": true, "releases": true, "tags": true, "branches": true, "actions": true,
	"wiki": true, "compare": true, "settings": true, "raw": true, "tree": true,
}

// Parse normalises input to a Source. It accepts owner/repo and
// owner/repo/subpath (GitHub), https and http URLs, GitHub tree URLs
// (/tree/<ref>/<subpath>), GitLab tree URLs (/-/tree/<ref>/<subpath>),
// ssh:// and git:// URLs, the user@host:path SSH shorthand, file:// URLs,
// and any of these with a #ref fragment. On GitHub the repository is
// owner/repo and what follows is the subpath; elsewhere the repository
// path ends at /-/ or at a segment named *.git, and what follows is the
// subpath. The host is lowercased and loses its trailing dot and a port
// that is its scheme's default, a .git suffix and a trailing slash are
// dropped, the path keeps its percent escapes, and an embedded user or
// token is removed. Anything else is an ErrForm. The canonical URL of what
// Parse accepts parses back to itself: it is the source's identity, printed
// by source list and read back by source remove and source skills.
func Parse(input string) (Source, error) {
	var s Source
	raw := strings.TrimSpace(input)
	// Every message below names what was given, and what was given may carry
	// a token, so each one names the redacted form instead.
	safe := redact(raw)
	if raw == "" {
		return s, fmt.Errorf("%w: empty", ErrForm)
	}
	raw, fragment, hasFragment := strings.Cut(raw, "#")
	if hasFragment {
		if fragment == "" || !validRef(fragment) {
			return s, fmt.Errorf("%w: %s does not end in a ref git accepts", ErrForm, safe)
		}
		s.Ref = fragment
	}

	switch {
	case strings.Contains(raw, "://"):
		if err := s.fromURL(raw, safe); err != nil {
			return Source{}, err
		}
	case scpLike.MatchString(raw):
		m := scpLike.FindStringSubmatch(raw)
		if err := s.fromURL("ssh://"+m[1]+"@"+m[2]+"/"+strings.TrimPrefix(m[3], "/"), safe); err != nil {
			return Source{}, err
		}
	case shorthand.MatchString(raw):
		if err := s.fromURL("https://github.com/"+raw, safe); err != nil {
			return Source{}, err
		}
	default:
		return s, fmt.Errorf("%w: %s", ErrForm, safe)
	}
	return s, nil
}

// redact replaces the userinfo of an input with ***, so that a password or
// token someone put in a URL never reaches an error message, a log line, an
// event or a terminal. It is for display only; parsing reads the original.
func redact(input string) string {
	prefix, rest := "", input
	if i := strings.Index(input, "://"); i >= 0 {
		prefix, rest = input[:i+3], input[i+3:]
	}
	authority := rest
	if i := strings.Index(rest, "/"); i >= 0 {
		authority = rest[:i]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return input
	}
	return prefix + "***@" + rest[at+1:]
}

// validRef accepts the refs a pin can be: the names git check-ref-format
// allows for a one-level branch or tag, plus a commit id. A ref git refuses
// is a usage error; accepting it here would defer it to a fetch error with
// an unrelated hint about credential helpers, so the rules are git's own,
// neither wider nor narrower: a commit id, release/1.x and a unicode name
// are all refs git allows.
func validRef(ref string) bool {
	if ref == "" || ref == "@" {
		return false
	}
	// git check-ref-format itself does not accept a leading dash, and a ref
	// that starts with one is read as an option wherever the refspec built
	// from it reaches a git command line.
	if strings.HasPrefix(ref, "-") {
		return false
	}
	if strings.ContainsAny(ref, " ~^:?*[\\") || strings.Contains(ref, "..") ||
		strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") ||
		strings.Contains(ref, "//") || strings.Contains(ref, "@{") {
		return false
	}
	for _, r := range ref {
		// No ASCII control character and no DEL, which covers a tab and a
		// bare newline; everything above them, unicode included, is a ref
		// git accepts.
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, component := range strings.Split(ref, "/") {
		// No slash-separated component may begin with a dot or end in .lock.
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

// fromURL fills s from a URL with a scheme. The tree ref of a GitHub or
// GitLab URL becomes the pin unless a fragment already named one.
func (s *Source) fromURL(raw, display string) error {
	u, err := url.Parse(raw)
	if err != nil {
		// A url.Error repeats the whole URL, credential included; its inner
		// error is the reason alone.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("%w: %v: %s", ErrForm, err, display)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "https", "http", "ssh", "git", "file":
	default:
		return fmt.Errorf("%w: unsupported scheme %q", ErrForm, u.Scheme)
	}
	if u.User != nil {
		_, hasPassword := u.User.Password()
		if scheme == "ssh" && u.User.Username() != "" {
			// git@ is the address of an SSH server, not a credential; a password is.
			s.Stripped = hasPassword
			u.User = url.User(u.User.Username())
		} else {
			s.Stripped = true
			u.User = nil
		}
	}
	// One address, one identity: the host is lowercased, the trailing dots
	// of a fully qualified name are dropped, and a port that is the
	// scheme's default is dropped, so that github.com, GitHub.com,
	// github.com. and github.com:443 are one source and not four.
	hostname := strings.TrimRight(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if port == defaultPorts[scheme] {
		port = ""
	}
	host := hostname
	switch {
	case strings.Contains(hostname, ":"):
		// Only an IP address may hold a colon. Writing an unbracketed one
		// back bracketed would make a canonical URL that does not parse, so
		// it is checked here and rendered in one spelling per address, with
		// the zone separator escaped the way a URL carries it.
		addr, aerr := netip.ParseAddr(hostname)
		if aerr != nil {
			return fmt.Errorf("%w: invalid host in %s", ErrForm, display)
		}
		hostname = addr.String()
		host = "[" + strings.ReplaceAll(hostname, "%", "%25") + "]"
	case !nameHost(hostname):
		// url.Parse decodes the host, so a host written with an escape in
		// it, http://%25/x, would be spelled back raw and the canonical URL
		// would not parse. Only a name reaches a git server anyway.
		return fmt.Errorf("%w: invalid host in %s", ErrForm, display)
	}
	if port != "" {
		host += ":" + port
	}
	if scheme == "file" && hostname == "localhost" && port == "" {
		host = ""
	}
	if scheme != "file" && host == "" {
		return fmt.Errorf("%w: no host in %s", ErrForm, display)
	}
	segments := splitPath(u.EscapedPath())
	if len(segments) == 0 {
		return fmt.Errorf("%w: no repository path in %s", ErrForm, display)
	}
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q in the path", ErrForm, seg)
		}
	}
	// The case of the path is kept as it was given, on GitHub as anywhere
	// else, although GitHub is case insensitive in owner and repository
	// name: Owner/Repo and owner/repo are one repository there, so keeping
	// both spellings mints two ids, two remotes and two refs for it. Folded
	// here it would be worse. Case folding is a property of a host, not of
	// a URL: it cannot be read off the string, only known of one forge, so
	// folding github.com alone leaves the rule inconsistent at the point
	// users most expect it, the same repository behind a GitHub Enterprise
	// host keeping its case. The canonical URL is also a stored identity,
	// in the machine settings and in the remote and ref names derived from
	// it, and its last segment is the name a skill at the repository root
	// takes when its frontmatter has none, so folding would rename skills
	// and orphan the remotes and refs of sources already added. Both
	// spellings clone, and each one round trips to itself, so this is a
	// duplicate rather than a wrong URL, and the settings already reserve
	// an alias field for mapping a second URL onto the canonical one: that
	// is where the duplicate belongs.
	var repo, rest []string
	switch hostname {
	case "github.com", "www.github.com":
		if len(segments) < 2 {
			return fmt.Errorf("%w: a GitHub URL needs owner/repo", ErrForm)
		}
		repo, rest = segments[:2], segments[2:]
		if len(rest) > 0 && githubPages[rest[0]] {
			if rest[0] != "tree" {
				return fmt.Errorf("%w: a GitHub %s URL; use the repository or a tree URL", ErrForm, rest[0])
			}
			if len(rest) < 2 {
				return fmt.Errorf("%w: a tree URL needs a ref", ErrForm)
			}
			if err := s.treeRef(rest[1]); err != nil {
				return err
			}
			rest = rest[2:]
		}
	default:
		// Elsewhere the repository path ends at the /-/ separator of a
		// GitLab tree URL or at the first segment named *.git; without
		// either the whole path is the repository.
		repo, rest = segments, nil
		for i, seg := range segments {
			if seg == "-" {
				repo, rest = segments[:i], segments[i+1:]
				if len(rest) < 2 || rest[0] != "tree" {
					return fmt.Errorf("%w: only a tree URL is accepted after /-/", ErrForm)
				}
				if err := s.treeRef(rest[1]); err != nil {
					return err
				}
				rest = rest[2:]
				break
			}
			if strings.HasSuffix(seg, ".git") && len(seg) > len(".git") {
				repo, rest = segments[:i+1], segments[i+1:]
				break
			}
		}
		if len(repo) == 0 {
			return fmt.Errorf("%w: no repository path in %s", ErrForm, display)
		}
	}
	s.Subpath = strings.Join(rest, "/")
	// A segment is checked for . and .. before it is decoded into the
	// subpath, so skills%2F..%2F.. reaches here as skills/../..; the
	// subpath is held to the rule of a tree entry, which git would
	// otherwise resolve through a source's own .. entries.
	if s.Subpath != "" && CheckPath(s.Subpath) != nil {
		return fmt.Errorf("%w: the path %q inside the repository is not one agentx reads", ErrForm, s.Subpath)
	}
	if scheme == "file" {
		// A path on disk is taken as it is: a .git directory is named so.
		s.URL = "file://" + host + "/" + joinPath(repo)
		return nil
	}
	// Every .git suffix goes, not one: trimming a.git.git once leaves
	// a.git, which a second parse trims again to a, so the canonical URL
	// would not be the identity of the source it names.
	last := repo[len(repo)-1]
	for strings.HasSuffix(last, ".git") {
		last = strings.TrimSuffix(last, ".git")
	}
	// Dropping the suffix can uncover what the segment check above refused:
	// ..git would leave .., a path that names another repository and a
	// canonical URL this very function rejects.
	if last == "" || last == "." || last == ".." {
		return fmt.Errorf("%w: no repository name in %s", ErrForm, display)
	}
	repo[len(repo)-1] = last
	if u.User != nil {
		// User.String escapes the name, so that a user carrying an @ or a
		// slash does not turn the canonical URL into a different one.
		host = u.User.String() + "@" + host
	}
	s.URL = scheme + "://" + host + "/" + joinPath(repo)
	return nil
}

// treeRef records the ref of a tree URL, which a fragment overrides only
// when both name the same ref.
func (s *Source) treeRef(ref string) error {
	if !validRef(ref) {
		return fmt.Errorf("%w: the URL names ref %q, which git does not accept", ErrForm, ref)
	}
	if s.Ref != "" && s.Ref != ref {
		return fmt.Errorf("%w: the URL names ref %q and the fragment %q", ErrForm, ref, s.Ref)
	}
	s.Ref = ref
	return nil
}

// splitPath splits the escaped path of a URL into its non-empty segments,
// decoded. It reads the escaped form so that a segment holding an encoded
// slash stays one segment, and so that joinPath can write the segments back
// escaped: splitting the decoded path and joining it raw turns
// file:///tmp/re%23po.git into file:///tmp/re#po.git, which parses back as
// the repository /tmp/re pinned to the ref po.git, a different source.
func splitPath(escaped string) []string {
	var segments []string
	for _, seg := range strings.Split(escaped, "/") {
		if seg == "" {
			continue
		}
		// url.Parse rejects a bad escape before this, so the decode holds;
		// an undecodable segment is kept as it stands rather than dropped.
		if decoded, err := url.PathUnescape(seg); err == nil {
			seg = decoded
		}
		segments = append(segments, seg)
	}
	return segments
}

// joinPath writes decoded path segments back as the path of a URL. Escaping
// each segment is what makes the canonical URL parse back to itself: the
// characters that would otherwise start a fragment, a query or a new
// segment are encoded, and the encoding is idempotent, so a second parse
// yields the same segments again.
func joinPath(segments []string) string {
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	return strings.Join(escaped, "/")
}

// nameHost reports whether a host is a name a URL carries as it stands: the
// characters of a DNS label, the dots between labels, and anything above
// ASCII, which an internationalised name is written in. The empty host of a
// file URL is one too; a host that is an IP literal is checked as an
// address instead.
func nameHost(host string) bool {
	for _, r := range host {
		switch {
		case r >= 0x80:
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// defaultPorts is the port each scheme connects to when a URL names none.
var defaultPorts = map[string]string{
	"https": "443",
	"http":  "80",
	"ssh":   "22",
	"git":   "9418",
}

// RepoName is the last segment of a canonical URL, decoded: the name a
// skill at the repository root takes when its frontmatter has none. The
// canonical URL escapes its path, and a percent escape is not part of the
// name; that name is also the first field of a skill's content hash, so it
// is the text, not its encoding, that is returned.
func RepoName(canonical string) string {
	base := path.Base(strings.TrimSuffix(canonical, "/"))
	decoded, err := url.PathUnescape(base)
	if err != nil {
		return base
	}
	return decoded
}
