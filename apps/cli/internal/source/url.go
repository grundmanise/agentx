// Package source parses source URLs and keeps sources in the account repo:
// one named remote and one ref per source, fetched without blobs, plus the
// SKILL.md blobs of the skills inside.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
// subpath. The host is lowercased, a .git suffix and a trailing slash are
// dropped, and an embedded user or token is removed. Anything else is an
// ErrForm.
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

// validRef accepts the refs a pin can be: what git check-ref-format allows
// in a branch or tag name, plus a commit id.
func validRef(ref string) bool {
	if strings.ContainsAny(ref, " \t~^:?*[\\") || strings.Contains(ref, "..") ||
		strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") ||
		strings.Contains(ref, "//") || strings.Contains(ref, "@{") || ref == "@" {
		return false
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
	host := strings.ToLower(u.Host)
	if scheme == "file" && host == "localhost" {
		host = ""
	}
	if scheme != "file" && host == "" {
		return fmt.Errorf("%w: no host in %s", ErrForm, display)
	}
	segments := splitPath(u.Path)
	if len(segments) == 0 {
		return fmt.Errorf("%w: no repository path in %s", ErrForm, display)
	}
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q in the path", ErrForm, seg)
		}
	}
	var repo, rest []string
	switch u.Hostname() {
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
	if scheme == "file" {
		// A path on disk is taken as it is: a .git directory is named so.
		s.URL = "file://" + host + "/" + strings.Join(repo, "/")
		return nil
	}
	last := strings.TrimSuffix(repo[len(repo)-1], ".git")
	if last == "" {
		return fmt.Errorf("%w: no repository name in %s", ErrForm, display)
	}
	repo[len(repo)-1] = last
	if u.User != nil {
		host = u.User.Username() + "@" + host
	}
	s.URL = scheme + "://" + host + "/" + strings.Join(repo, "/")
	return nil
}

// treeRef records the ref of a tree URL, which a fragment overrides only
// when both name the same ref.
func (s *Source) treeRef(ref string) error {
	if s.Ref != "" && s.Ref != ref {
		return fmt.Errorf("%w: the URL names ref %q and the fragment %q", ErrForm, ref, s.Ref)
	}
	s.Ref = ref
	return nil
}

// splitPath splits a URL path into its non-empty segments.
func splitPath(p string) []string {
	var segments []string
	for _, seg := range strings.Split(p, "/") {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	return segments
}

// RepoName is the last segment of a canonical URL: the name a skill at the
// repository root takes when its frontmatter has none.
func RepoName(canonical string) string {
	return path.Base(strings.TrimSuffix(canonical, "/"))
}
