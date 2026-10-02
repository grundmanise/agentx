package source

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Address is where a git URL reaches, in the parts two URLs of one
// repository are compared by: the transport, the user, the host and the
// path. It reads every form git does for a remote it pushes to, the forms
// Parse reads and a path on disk among them, and keeps no subpath and no
// ref: an address is a whole repository.
type Address struct {
	Scheme string // https, http, ssh, git or file; the SSH shorthand and git+ssh and ssh+git read as ssh, a path on disk as file
	User   string // the user of an SSH address, "" for none
	Host   string // lowercased, without a trailing dot; "" for a path on disk
	Port   string // "" for the scheme's default
	Path   string // the repository path, decoded, without a leading or trailing slash or a .git suffix; its case is kept
}

// ErrCredential is the error of a URL that carries a password or a token:
// a password anywhere, or a user over a transport other than SSH, where it
// is a token.
var ErrCredential = errors.New("the URL carries a password or a token")

// scpAddress is git's SSH shorthand, [user@]host:path, whose host holds no
// slash; git reads a colon after a slash as part of a path on disk.
var scpAddress = regexp.MustCompile(`^(?:([^@/:\s]+)@)?([^@/:\s]+):(.+)$`)

// ParseAddress reads raw as git reads the URL of a remote: a URL with a
// scheme, the SSH shorthand [user@]host:path or an absolute path. It never
// repeats raw in an error, since what it refuses can be a credential.
// Pure.
func ParseAddress(raw string) (Address, error) {
	switch {
	case raw == "" || strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, unicode.IsControl) >= 0:
		return Address{}, fmt.Errorf("%w: empty, or holds a space or a control character", ErrForm)
	case strings.HasPrefix(raw, "-"):
		return Address{}, fmt.Errorf("%w: starts with a dash", ErrForm)
	case strings.Contains(raw, "#"):
		return Address{}, fmt.Errorf("%w: names a ref with #", ErrForm)
	}
	var a Address
	var path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return Address{}, fmt.Errorf("%w: not a URL git reads", ErrForm)
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
				return Address{}, ErrCredential
			}
			a.User = u.User.Username()
		}
		a.Host = strings.TrimRight(strings.ToLower(u.Hostname()), ".")
		if a.Port = u.Port(); a.Port == defaultPorts[a.Scheme] {
			a.Port = ""
		}
		if a.Scheme == "file" && a.Host == "localhost" && a.Port == "" {
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
			return Address{}, ErrCredential
		}
		m := scpAddress.FindStringSubmatch(raw)
		if m == nil {
			return Address{}, fmt.Errorf("%w: neither a URL, the SSH shorthand nor an absolute path", ErrForm)
		}
		a.Scheme, a.User, a.Host, path = "ssh", m[1], strings.TrimRight(strings.ToLower(m[2]), "."), m[3]
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
	last := len(segments) - 1
	for strings.HasSuffix(segments[last], ".git") && len(segments[last]) > len(".git") {
		segments[last] = strings.TrimSuffix(segments[last], ".git")
	}
	a.Path = strings.Join(segments, "/")
	return a, nil
}

// SameTransport reports whether a and b reach their repositories the same
// way: the same transport, user, host and port. Two URLs of one repository
// that do are spelled differently and nothing more, as https://host/a and
// https://host/a.git are, and git@host:a and ssh://git@host/a.
func SameTransport(a, b Address) bool {
	return a.Scheme == b.Scheme && a.User == b.User && a.Host == b.Host && a.Port == b.Port
}

// Kinship is how much of two addresses names one repository.
type Kinship int

const (
	// OtherRepository: the paths differ, so the addresses name two
	// repositories whatever their hosts.
	OtherRepository Kinship = iota
	// OtherHost: the same path on hosts that do not resolve to one host.
	// They may still be one repository, through a name agentx cannot see
	// through, so a caller warns rather than refuses.
	OtherHost
	// SameRepository: the same path on the same host.
	SameRepository
)

// sameHosts are the hosts a forge serves one repository under, each mapped
// to the host it is known by: the SSH endpoint over port 443 that a
// firewall leaves open, and the www name of a web host.
var sameHosts = map[string]string{
	"ssh.github.com":       "github.com",
	"www.github.com":       "github.com",
	"altssh.gitlab.com":    "gitlab.com",
	"altssh.bitbucket.org": "bitbucket.org",
}

// Compare tells whether a and b name one repository. The paths decide
// first, compared as they are written but for a .git suffix and the
// slashes around them; the hosts then, each SSH host first through
// resolve, which reads the host name an SSH configuration gives an alias
// (nil reads every host as itself), then through the hosts a forge serves
// one repository under. The scheme, the user and the port do not decide:
// one repository is reached over HTTPS and over SSH alike. Pure.
func Compare(a, b Address, resolve func(host string) string) Kinship {
	if a.Path != b.Path {
		return OtherRepository
	}
	if canonicalHost(a, resolve) != canonicalHost(b, resolve) {
		return OtherHost
	}
	return SameRepository
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
