package mcp

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// NormalizeURL renders the address of a remote server so that two spellings
// of one endpoint compare equal: lowercase scheme and host, no user
// information, no default port, no trailing slash, no query or fragment.
// ok is false when raw has no real DNS host: no host, localhost or an IP
// address name a server local to one machine.
func NormalizeURL(raw string) (normalized string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || net.ParseIP(host) != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host + strings.TrimRight(u.Path, "/"), true
}

// Package reports the registry and package a local server resolves to:
// `npx [flags] <pkg>[@version]` is npm, `uvx [flags] <pkg>[specifier]` and
// `pipx run [flags] <pkg>[specifier]` are pypi, `docker run [flags] <image>[:tag|@digest]`
// is docker. The version, specifier, tag or digest is dropped. ok is false
// for any other command.
func Package(command string, args []string) (registry, pkg string, ok bool) {
	switch filepath.Base(command) {
	case "npx":
		pkg = positional(args, nil)
		if i := strings.LastIndex(pkg, "@"); i > 0 {
			pkg = pkg[:i]
		}
		registry = "npm"
	case "uvx":
		pkg = pythonPackage(positional(args, nil))
		registry = "pypi"
	case "pipx":
		if len(args) > 0 && args[0] == "run" {
			pkg = pythonPackage(positional(args[1:], nil))
			registry = "pypi"
		}
	case "docker":
		if len(args) > 0 && args[0] == "run" {
			pkg = image(positional(args[1:], dockerValueFlags))
			registry = "docker"
		}
	}
	return registry, pkg, pkg != ""
}

// dockerValueFlags are the `docker run` flags whose value is the next argument.
var dockerValueFlags = map[string]bool{
	"-e": true, "--env": true, "--env-file": true, "-v": true, "--volume": true, "--mount": true,
	"-p": true, "--publish": true, "--name": true, "-w": true, "--workdir": true,
	"--network": true, "--entrypoint": true, "-u": true, "--user": true, "--platform": true,
	"-l": true, "--label": true, "--add-host": true, "-h": true, "--hostname": true,
}

// positional is the first argument that is not a flag or the value of one
// of the flags in valueFlags.
func positional(args []string, valueFlags map[string]bool) string {
	for i := 0; i < len(args); i++ {
		if valueFlags[args[i]] {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

// pythonPackage is the name before any version specifier or extras.
func pythonPackage(spec string) string {
	if i := strings.IndexAny(spec, "=<>~!@["); i >= 0 {
		return spec[:i]
	}
	return spec
}

// image is the image reference without its tag or digest.
func image(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}
