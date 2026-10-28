package scan

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeSVG covers what the sanitizer removes from an SVG, every
// script element, every event handler attribute and every href that names
// the javascript scheme however it is spelled, and that it leaves the rest
// of the document as it was.
func TestSanitizeSVG(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain document kept", `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`, `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`},
		{"script element", `<svg><script>alert(1)</script><rect/></svg>`, `<svg><rect/></svg>`},
		{"script with attributes and case", `<svg><SCRIPT type="text/javascript">x()</Script ><rect/></svg>`, `<svg><rect/></svg>`},
		{"self-closing script", `<svg><script href="x.js"/><rect/></svg>`, `<svg><rect/></svg>`},
		{"unterminated script to the end", "<svg><rect/><script>evil(", `<svg><rect/>`},
		{"event handlers", `<svg onload="run()"><rect onclick='run()' fill="red" ONMOUSEOVER=run() /></svg>`, `<svg><rect fill="red" /></svg>`},
		{"javascript href", `<svg><a href="javascript:alert(1)"><text>x</text></a></svg>`, `<svg><a><text>x</text></a></svg>`},
		{"xlink href with case and spaces", `<svg><a xlink:href=" JavaScript:alert(1)">x</a></svg>`, `<svg><a>x</a></svg>`},
		{"entity-encoded scheme", `<svg><a href="&#106;avascript:alert(1)">x</a></svg>`, `<svg><a>x</a></svg>`},
		{"tab inside the scheme", "<svg><a href='java\tscript:alert(1)'>x</a></svg>", `<svg><a>x</a></svg>`},
		{"other hrefs kept", `<svg><use href="#shape"/><a xlink:href="https://example.com">x</a><image href="data:image/png;base64,AA=="/></svg>`, `<svg><use href="#shape"/><a xlink:href="https://example.com">x</a><image href="data:image/png;base64,AA=="/></svg>`},
		{"a > inside a value hides nothing", `<svg><a href="javascript:1>2" onclick="x">x</a></svg>`, `<svg><a>x</a></svg>`},
		{"text content kept", `<svg><text>onclick="not an attribute"</text></svg>`, `<svg><text>onclick="not an attribute"</text></svg>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(sanitizeSVG([]byte(tt.in))); got != tt.want {
				t.Errorf("sanitizeSVG(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// png is the smallest PNG signature a scan recognises; the rest of the
// image is not read.
const png = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

// TestPluginIcon covers the checks an icon passes before its bytes reach the
// inventory: the manifest path rules, that it resolves inside the plugin
// through any symlink, the size limit and the three image formats.
func TestPluginIcon(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "plugin")
	files := map[string]string{
		"plugin/logo.png":        png,
		"plugin/photo.jpg":       "\xFF\xD8\xFF\xE0JFIF",
		"plugin/assets/icon.svg": `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="x()"><script>x()</script><rect/></svg>`,
		"plugin/notes.txt":       "hello",
		"plugin/big.png":         png + strings.Repeat("a", maxIconSize),
		"plugin/exact.png":       png + strings.Repeat("a", maxIconSize-len(png)),
		"outside/logo.png":       png,
	}
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside", "logo.png"), filepath.Join(dir, "linked.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "logo.png"), filepath.Join(dir, "inside.png")); err != nil {
		t.Fatal(err)
	}
	dataURL := func(mime, content string) string {
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(content))
	}
	tests := []struct {
		name string
		rel  string
		bare bool
		want string
		warn string // a substring of the one warning, "" for none
	}{
		{name: "png inside", rel: "./logo.png", want: dataURL("image/png", png)},
		{name: "jpeg inside", rel: "./photo.jpg", want: dataURL("image/jpeg", "\xFF\xD8\xFF\xE0JFIF")},
		{name: "svg sanitised", rel: "./assets/icon.svg", want: dataURL("image/svg+xml", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)},
		{name: "symlink inside followed", rel: "./inside.png", want: dataURL("image/png", png)},
		{name: "exactly the limit", rel: "./exact.png", want: dataURL("image/png", files["plugin/exact.png"])},
		{name: "nothing named", rel: ""},
		{name: "a URL is not a file", rel: "https://example.com/logo.png"},
		{name: "bare path refused", rel: "logo.png", warn: `icon path "logo.png" must start with ./`},
		{name: "bare path allowed", rel: "logo.png", bare: true, want: dataURL("image/png", png)},
		{name: "escapes with ..", rel: "./../outside/logo.png", warn: `icon path "./../outside/logo.png" must start with ./ and stay inside the plugin, skipped`},
		{name: "escapes through a symlink", rel: "./linked.png", warn: `icon path "./linked.png" resolves outside the plugin, skipped`},
		{name: "missing", rel: "./gone.png", warn: `icon "./gone.png": `},
		{name: "a directory", rel: "./assets", warn: `icon "./assets" is not a file, skipped`},
		{name: "over the limit", rel: "./big.png", warn: `icon "./big.png" is 65552 bytes, over the 64 KiB limit, skipped`},
		{name: "not an image", rel: "./notes.txt", warn: `icon "./notes.txt" is not a PNG, a JPEG or an SVG, skipped`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var warnings []string
			got := pluginIcon(tt.rel, "plugin.json", "icon", dir, tt.bare, func(w string) { warnings = append(warnings, w) })
			if got != tt.want {
				t.Errorf("pluginIcon(%q) = %q, want %q", tt.rel, got, tt.want)
			}
			switch {
			case tt.warn == "" && len(warnings) > 0:
				t.Errorf("warnings = %v, want none", warnings)
			case tt.warn != "" && (len(warnings) != 1 || !strings.Contains(warnings[0], "plugin.json: "+tt.warn)):
				t.Errorf("warnings = %v, want one containing %q", warnings, tt.warn)
			}
		})
	}
}
