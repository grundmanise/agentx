// Package weburl turns the addresses agentx reads of a skill's origin, a
// plugin manifest's repository or homepage, a marketplace's git URL or a
// source's canonical URL, into the web URL a browser opens for them.
package weburl

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// limit is the longest web URL agentx keeps.
const limit = 2048

var (
	// scpLike is [user@]host:path, the SSH shorthand git accepts.
	scpLike = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.-]+):/?([^:]*)$`)
	// shorthand is owner/repo, which names a GitHub repository: an owner
	// of letters, digits and hyphens, and a repository of those, dots and
	// underscores that is neither . nor ..
	shorthand = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/(?:[A-Za-z0-9_-][A-Za-z0-9._-]*|\.[A-Za-z0-9_-][A-Za-z0-9._-]*|\.\.[A-Za-z0-9._-]+)$`)
	// hostName is a DNS name of at least two labels.
	hostName = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)+$`)
)

// Of is the address a browser opens for raw. An http or https URL is kept
// as it stands, without its user or password, a trailing slash or a .git
// suffix. A git address of a named host, an ssh:// or git:// URL, a
// git+https:// or git+ssh:// URL, [user@]host:path, or owner/repo for
// GitHub, see GitHub, is the https URL of the same path on that host, which is where
// GitHub, GitLab and their like show the repository. Anything else is "":
// a file URL, a path, a host that is no DNS name of two labels or more,
// text that is no URL, or one that holds whitespace or a control
// character. What Of returns, Is accepts. Pure.
func Of(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > limit || strings.IndexFunc(raw, notInURL) >= 0 {
		return ""
	}
	if len(raw) > 4 && strings.EqualFold(raw[:4], "git+") && strings.Contains(raw, "://") {
		raw = raw[4:] // npm's git+https:// and git's git+ssh://
	}
	var u *url.URL
	switch {
	case strings.Contains(raw, "://"):
		var err error
		if u, err = url.Parse(raw); err != nil {
			return ""
		}
		switch strings.ToLower(u.Scheme) {
		case "https", "http":
		case "ssh", "git":
			// The host a clone reaches over SSH or git serves its web pages
			// over https, on its default port.
			if !hostName.MatchString(u.Hostname()) {
				return ""
			}
			u.Scheme, u.Host = "https", u.Hostname()
		default:
			return ""
		}
	case shorthand.MatchString(raw):
		return GitHub(raw)
	case scpLike.MatchString(raw):
		m := scpLike.FindStringSubmatch(raw)
		if !hostName.MatchString(m[1]) {
			return ""
		}
		u = &url.URL{Scheme: "https", Host: m[1], Path: "/" + m[2]}
	default:
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.User = nil
	if escaped := u.EscapedPath(); u.RawQuery == "" && u.Fragment == "" {
		if trimmed := strings.TrimSuffix(strings.TrimRight(escaped, "/"), ".git"); trimmed != escaped {
			path, err := url.PathUnescape(trimmed)
			if err != nil {
				return ""
			}
			u.Path, u.RawPath = path, trimmed
		}
	}
	web := u.String()
	if !Is(web) {
		return ""
	}
	return web
}

// GitHub is the web URL of the GitHub repository owner/repo names, as a
// GitHub marketplace source or a manifest's repository writes it, without
// a .git suffix; "" when repo has another shape. Pure.
func GitHub(repo string) string {
	if !shorthand.MatchString(repo) {
		return ""
	}
	return "https://github.com/" + strings.TrimSuffix(repo, ".git")
}

// Is reports whether s is a web URL agentx writes down: an http or https
// URL, the scheme in lowercase, of a DNS name or an IP address, with no
// user or password, no whitespace and no control character, of at most
// 2048 bytes. Pure.
func Is(s string) bool {
	if len(s) > limit || strings.IndexFunc(s, notInURL) >= 0 || (!strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://")) {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" {
		return false
	}
	host := u.Hostname()
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return hostName.MatchString(host)
}

// notInURL reports whether r is whitespace or a control character, which
// no URL agentx keeps holds.
func notInURL(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }
