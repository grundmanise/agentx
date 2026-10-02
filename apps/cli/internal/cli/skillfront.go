package cli

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// errNotText is renameFrontmatter's refusal of a SKILL.md it cannot edit
// as text.
var errNotText = errors.New("SKILL.md is not UTF-8 text")

// errUnclosedFrontmatter is renameFrontmatter's refusal of a SKILL.md whose
// frontmatter block has no closing line, which no reader takes for
// frontmatter and which a new name line would not fix.
var errUnclosedFrontmatter = errors.New("the frontmatter of SKILL.md is not closed by a --- line")

// renameFrontmatter is md, the bytes of a SKILL.md, with the name its
// frontmatter gives the skill set to name. It is an ordinary edit, the one
// a person would make: the value of the top-level name key in the leading
// --- block is replaced, the lines a value continues on included, a second
// name key, which YAML refuses, is dropped, and every other byte is kept,
// comments, order, quoting and line endings alike. A block with no name
// key gets one as its first line, and a file with no block gets one
// holding the name alone. The name is written as a YAML
// string that reads back as itself, see yamlString. A SKILL.md that is not
// UTF-8 text, or whose block is not closed, is refused rather than guessed
// at.
func renameFrontmatter(md []byte, name string) ([]byte, error) {
	if !utf8.Valid(md) {
		return nil, errNotText
	}
	text := string(md)
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || !fenceLine(lines[0]) {
		return []byte("---\nname: " + yamlString(name) + "\n---\n" + text), nil
	}
	eol := "\n"
	if strings.HasSuffix(lines[0], "\r\n") {
		eol = "\r\n"
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if fenceLine(lines[i]) {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, errUnclosedFrontmatter
	}
	nameLine := "name: " + yamlString(name)
	var out strings.Builder
	out.WriteString(lines[0])
	renamed := false
	for i := 1; i < end; i++ {
		line := lines[i]
		if !nameKey(line) {
			out.WriteString(line)
			continue
		}
		// The lines a value continues on are indented below its key: a
		// block scalar's, or a plain scalar folded over several lines.
		for i+1 < end && continued(lines[i+1]) {
			i++
		}
		if !renamed {
			out.WriteString(nameLine + lineEnding(line, eol))
			renamed = true
		}
	}
	if !renamed {
		return []byte(lines[0] + nameLine + eol + strings.Join(lines[1:], "")), nil
	}
	out.WriteString(strings.Join(lines[end:], ""))
	return []byte(out.String()), nil
}

// fenceLine reports whether line opens or closes a frontmatter block, as
// the scan reads one: three dashes and nothing else but trailing blanks.
func fenceLine(line string) bool {
	return strings.TrimRight(line, " \t\r\n") == "---"
}

// nameKey reports whether line holds the top-level name key: not indented,
// and the key itself, not one that starts with it.
func nameKey(line string) bool {
	rest, ok := strings.CutPrefix(line, "name")
	return ok && strings.HasPrefix(strings.TrimLeft(rest, " \t"), ":")
}

// continued reports whether line continues the value of the key above it,
// which it does when it is indented below the key.
func continued(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

// lineEnding is the ending of line, or eol for a last line that has none.
func lineEnding(line, eol string) string {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(line, "\n"):
		return "\n"
	}
	return eol
}
