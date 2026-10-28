package scan

import (
	"bytes"
	"encoding/base64"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// maxIconSize is the largest icon file a plugin node carries, 64 KiB: the
// bytes travel in every inventory event.
const maxIconSize = 64 * 1024

// pluginIcon is the icon of the plugin at dir as a data URL, when rel, the
// image the manifest's field names, is a file inside the plugin of at most
// maxIconSize bytes holding a PNG, a JPEG or an SVG, the SVG with its
// scripts removed; else "". A URL is not an image inside the plugin and is
// silently nothing; a path that leaves the plugin, directly or through a
// symlink, a file that cannot be read, one over the limit and one that is
// none of the three formats are each a warning naming the manifest.
func pluginIcon(rel, manifest, field, dir string, bare bool, warn func(string)) string {
	if rel == "" || strings.Contains(rel, "://") {
		return ""
	}
	if !manifestPath(rel, manifest, field, bare, warn) {
		return ""
	}
	path := filepath.Join(dir, rel)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + ": " + err.Error() + ", skipped")
		return ""
	}
	if root, err := filepath.EvalSymlinks(dir); err != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		warn(manifest + ": " + field + " path " + strconv.Quote(rel) + " resolves outside the plugin, skipped")
		return ""
	}
	info, err := os.Stat(real)
	if err != nil {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + ": " + err.Error() + ", skipped")
		return ""
	}
	if !info.Mode().IsRegular() {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + " is not a file, skipped")
		return ""
	}
	if info.Size() > maxIconSize {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + " is " + strconv.FormatInt(info.Size(), 10) + " bytes, over the 64 KiB limit, skipped")
		return ""
	}
	b, err := os.ReadFile(real)
	if err != nil {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + ": " + err.Error() + ", skipped")
		return ""
	}
	mime, ok := imageType(b)
	if !ok {
		warn(manifest + ": " + field + " " + strconv.Quote(rel) + " is not a PNG, a JPEG or an SVG, skipped")
		return ""
	}
	if mime == "image/svg+xml" {
		b = sanitizeSVG(b)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// svgRoot finds the root element of an SVG document, after any byte order
// mark, XML declaration, comment, processing instruction or doctype.
var svgRoot = regexp.MustCompile(`(?is)\A\x{FEFF}?(?:\s|<\?.*?\?>|<!--.*?-->|<!DOCTYPE[^>]*>)*<svg[\s>]`)

// imageType is the media type of an image by its content: PNG and JPEG by
// their signatures, SVG by its root element.
func imageType(b []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(b, []byte("\xFF\xD8\xFF")):
		return "image/jpeg", true
	case svgRoot.Match(b):
		return "image/svg+xml", true
	}
	return "", false
}

var (
	// svgScript is a script element with its content, a self-closing one, or
	// one left open to the end of the document.
	svgScript = regexp.MustCompile(`(?is)<script\b[^>]*/>|<script\b[^>]*>.*?</script\s*>|<script\b.*`)
	// svgAttr is one attribute inside a tag, with its value quoted either
	// way or bare; group 1 is its name and group 2 its value.
	svgAttr = regexp.MustCompile(`(?is)\s+([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	// svgTag is a start tag with its attributes, a quoted value free to
	// hold a >.
	svgTag = regexp.MustCompile(`(?s)<[a-zA-Z_:](?:[^>"']|"[^"]*"|'[^']*')*>`)
	// controls are the characters a browser drops from a URL before it
	// reads its scheme.
	controls = regexp.MustCompile(`[\x00-\x20]+`)
)

// sanitizeSVG removes what could run as code from an SVG document: every
// script element, every on* event handler attribute and every href or
// xlink:href attribute whose value is not one safeHref allows. Everything
// else is kept byte for byte.
func sanitizeSVG(b []byte) []byte {
	b = svgScript.ReplaceAll(b, nil)
	return svgTag.ReplaceAllFunc(b, func(tag []byte) []byte {
		return svgAttr.ReplaceAllFunc(tag, func(attr []byte) []byte {
			m := svgAttr.FindSubmatch(attr)
			name := strings.ToLower(string(m[1]))
			value := string(m[2]) + string(m[3]) + string(m[4])
			switch {
			case strings.HasPrefix(name, "on"):
				return nil
			case (name == "href" || name == "xlink:href") && !safeHref(value):
				return nil
			}
			return attr
		})
	})
}

// safeHref reports whether an href value is one the icon keeps: once its
// entities are decoded, and the control characters and spaces a browser
// ignores are dropped, it is empty, a fragment, a relative reference with
// no scheme, or an http or https URL. Every other scheme, javascript, data,
// vbscript and anything unknown, is dropped: a list of what is allowed
// cannot miss a scheme the way a list of what is forbidden can.
func safeHref(value string) bool {
	v := strings.ToLower(controls.ReplaceAllString(html.UnescapeString(value), ""))
	if v == "" || strings.HasPrefix(v, "#") {
		return true
	}
	colon := strings.Index(v, ":")
	if colon < 0 {
		return true
	}
	if end := strings.IndexAny(v, "/?#"); end >= 0 && end < colon {
		return true // the colon is inside the path or the query: no scheme
	}
	scheme := v[:colon]
	return scheme == "http" || scheme == "https"
}
