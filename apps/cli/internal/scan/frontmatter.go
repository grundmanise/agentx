package scan

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// frontmatter is what the --- block at the top of SKILL.md says about a skill.
type frontmatter struct {
	name, description string
}

var errNoFrontmatter = errors.New("no frontmatter")

// parseFrontmatter reads name and description from the --- block at the top
// of SKILL.md. It understands `key: value` lines, quoted values, folded (>)
// and literal (|) block scalars and comments; nested values are skipped. An
// unusable block is an error and an empty frontmatter.
func parseFrontmatter(text string) (frontmatter, error) {
	var fm frontmatter
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return fm, errNoFrontmatter
	}
	var (
		key   string   // the key whose value continues on indented lines
		style byte     // '|', '>', 'n' for a nested value, or 0 for a plain scalar
		parts []string // the continuation lines collected so far
	)
	flush := func() {
		if key == "" || style == 'n' || len(parts) == 0 {
			return
		}
		sep := " "
		if style == '|' {
			sep = "\n"
		}
		fm.set(key, strings.TrimRight(strings.Join(parts, sep), " \t\n"))
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimRight(line, " \t") == "---" {
			flush()
			return fm, nil
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if key == "" {
				return frontmatter{}, fmt.Errorf("unparsable frontmatter at line %d", i+1)
			}
			if style != 'n' {
				parts = append(parts, trimmed)
			}
			continue
		}
		flush()
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(k, " \t") || k == "" {
			return frontmatter{}, fmt.Errorf("unparsable frontmatter at line %d", i+1)
		}
		key, style, parts = k, 0, nil
		v = strings.TrimSpace(v)
		switch v {
		case "|", "|-", "|+":
			style = '|'
		case ">", ">-", ">+":
			style = '>'
		case "":
			style = 'n' // a nested value: its indented lines are skipped
		default:
			parts = []string{unquote(v)}
		}
	}
	return frontmatter{}, errors.New("unparsable frontmatter, the --- block is not closed")
}

func (fm *frontmatter) set(key, value string) {
	switch key {
	case "name":
		fm.name = value
	case "description":
		fm.description = value
	}
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
