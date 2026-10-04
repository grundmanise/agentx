package source

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Address is where the canonical URL of a source reaches, in the parts
// that decide whether two sources name one repository: the host and the
// path. It keeps no subpath and no ref: an address is a whole repository.
type Address struct {
	Scheme string // https, http, ssh, git or file; the SSH shorthand and git+ssh and ssh+git read as ssh, a path on disk as file
	Host   string // lowercased, without a trailing dot; "" for a path on disk, whose file URL's host git ignores
	Path   string // the repository path, decoded, without a leading or trailing slash; its case is kept, and so is a .git suffix on disk alone
}

// scpAddress is git's SSH shorthand, [user@]host:path, whose host holds no
// slash; git reads a colon after a slash as part of a path on disk.
var scpAddress = regexp.MustCompile(`^(?:([^@/:\s]+)@)?([^@/:\s]+):(.+)$`)

// helperAddress is git's <transport>::<address>, which hands the address
// to the remote helper git-remote-<transport>: the transport is the
// characters a URL leaves unescaped, as git reads it.
var helperAddress = regexp.MustCompile(`^[A-Za-z0-9._~-]+::`)

// ParseAddress reads raw as git reads the URL of a remote: a URL with a
// scheme, the SSH shorthand [user@]host:path or an absolute path. A space
// inside raw is read as git reads it, as part of the path, and one at
// either end is refused. A URL with a query is refused: git reads a query
// over SSH and on disk as part of the path and sends one over HTTP before
// the path it appends, and a query is where a token rides. So is a URL
// that carries a password, or a user over a transport other than SSH,
// where it is a token. It never repeats raw in an error, since what it
// refuses can be a credential. Pure.
func ParseAddress(raw string) (Address, error) {
	switch {
	case raw == "" || strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, unicode.IsControl) >= 0:
		return Address{}, fmt.Errorf("%w: empty, has a space at either end, or holds a control character", ErrForm)
	case strings.HasPrefix(raw, "-"):
		return Address{}, fmt.Errorf("%w: starts with a dash", ErrForm)
	case strings.Contains(raw, "#"):
		return Address{}, fmt.Errorf("%w: names a ref with #", ErrForm)
	case helperAddress.MatchString(raw):
		return Address{}, fmt.Errorf("%w: unsupported scheme, the address of a remote helper", ErrForm)
	}
	var a Address
	var path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		switch {
		case err != nil || u.Opaque != "":
			return Address{}, fmt.Errorf("%w: not a URL git reads", ErrForm)
		case u.RawQuery != "" || u.ForceQuery:
			return Address{}, fmt.Errorf("%w: holds a query", ErrForm)
		}
		a.Scheme = strings.ToLower(u.Scheme)
		switch a.Scheme {
		case "git+ssh", "ssh+git":
			a.Scheme = "ssh"
		case "https", "http", "ssh", "git", "file":
		default:
			return Address{}, fmt.Errorf("%w: unsupported scheme %q", ErrForm, u.Scheme)
		}
		if u.User != nil {
			if _, password := u.User.Password(); password || a.Scheme != "ssh" {
				return Address{}, fmt.Errorf("%w: carries a password or a token", ErrForm)
			}
		}
		a.Host = strings.TrimRight(strings.ToLower(u.Hostname()), ".")
		if a.Scheme == "file" {
			// git reads a file URL's path on this machine whatever host it
			// names, so a file URL shares no host with a URL over the
			// network.
			a.Host = ""
		}
		if a.Scheme != "file" && a.Host == "" {
			return Address{}, fmt.Errorf("%w: no host", ErrForm)
		}
		path = u.Path
	case strings.HasPrefix(raw, "/"):
		a.Scheme, path = "file", raw
	default:
		// A colon in what stands before the @ is a password, which the
		// shorthand below would otherwise read as a host.
		if user, _, ok := strings.Cut(raw, "@"); ok && strings.Contains(user, ":") && !strings.Contains(user, "/") {
			return Address{}, fmt.Errorf("%w: carries a password or a token", ErrForm)
		}
		m := scpAddress.FindStringSubmatch(raw)
		if m == nil {
			return Address{}, fmt.Errorf("%w: neither a URL, the SSH shorthand nor an absolute path", ErrForm)
		}
		a.Scheme, a.Host, path = "ssh", strings.TrimRight(strings.ToLower(m[2]), "."), m[3]
	}
	var segments []string
	for _, seg := range strings.Split(path, "/") {
		switch seg {
		case "":
			continue
		case ".", "..":
			return Address{}, fmt.Errorf("%w: %q in the path", ErrForm, seg)
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return Address{}, fmt.Errorf("%w: no repository path", ErrForm)
	}
	// A server finds a repository with or without its .git suffix, while
	// on disk /srv/skills and /srv/skills.git can be two directories, so
	// the suffix goes everywhere but there, as Parse keeps it in a file URL.
	a.Path = strings.Join(segments, "/")
	if a.Scheme != "file" {
		a.Path = trimGit(a.Path)
	}
	return a, nil
}

// trimGit is path without the .git suffixes of its last segment, as a
// server finds the repository.
func trimGit(path string) string {
	dir, last := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, last = path[:i+1], path[i+1:]
	}
	for strings.HasSuffix(last, ".git") && len(last) > len(".git") {
		last = strings.TrimSuffix(last, ".git")
	}
	return dir + last
}

// sameHosts are the hosts a forge serves one repository under, each mapped
// to the host it is known by: the SSH endpoint over port 443 that a
// firewall leaves open, and the www name of a web host.
var sameHosts = map[string]string{
	"ssh.github.com":       "github.com",
	"www.github.com":       "github.com",
	"altssh.gitlab.com":    "gitlab.com",
	"altssh.bitbucket.org": "bitbucket.org",
}

// SameRepository tells whether a and b name one repository: the same path
// on the same host. The paths are compared as ParseAddress leaves them, but
// for the .git suffix of a path on disk beside a URL over the network,
// which a server finds the repository without; the hosts each SSH host
// first through resolve, which reads the host name an SSH configuration
// gives an alias (nil reads every host as itself), then through the hosts
// a forge serves one repository under. The same path on hosts that do not
// resolve to one host names two repositories, and so does a path on disk
// beside a URL over the network. The scheme does not decide: one
// repository is reached over HTTPS and over SSH alike. Pure.
func SameRepository(a, b Address, resolve func(host string) string) bool {
	if (a.Scheme == "file") != (b.Scheme == "file") {
		a.Path, b.Path = trimGit(a.Path), trimGit(b.Path)
	}
	return a.Path == b.Path && canonicalHost(a, resolve) == canonicalHost(b, resolve)
}

// canonicalHost is the host a's repository is known by.
func canonicalHost(a Address, resolve func(host string) string) string {
	host := a.Host
	if a.Scheme == "ssh" && resolve != nil && host != "" {
		host = strings.TrimRight(strings.ToLower(resolve(host)), ".")
	}
	if known, ok := sameHosts[host]; ok {
		return known
	}
	return host
}
