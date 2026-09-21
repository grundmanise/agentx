package scan

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
)

// frontmatter is what the --- block at the top of SKILL.md says about a skill.
type frontmatter struct {
	name, description string
}

var errNoFrontmatter = errors.New("no frontmatter")

// parseFrontmatter reads name and description from the --- block at the top
// of SKILL.md. The block is YAML and is parsed as such, so every scalar form
// the standard allows works: a plain value, a quoted one, a literal or folded
// block, and a plain value continued on the indented lines below its key. A
// key whose value is a mapping or a sequence contributes nothing, which is
// how a nested block such as metadata is skipped. An unusable block is an
// error and an empty frontmatter; the skill is then named after its
// directory.
func parseFrontmatter(text string) (frontmatter, error) {
	var fm frontmatter
	block, err := frontmatterBlock(text)
	if err != nil {
		return fm, err
	}
	if err := bounded(block); err != nil {
		return fm, err
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(block), &fields); err != nil {
		return fm, unparsable(err)
	}
	fm.name = scalar(fields["name"])
	fm.description = scalar(fields["description"])
	return fm, nil
}

// frontmatterBlock returns the lines between the opening --- of SKILL.md and
// the next one, without either.
func frontmatterBlock(text string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", errNoFrontmatter
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n"), nil
		}
	}
	return "", errors.New("unparsable frontmatter, the --- block is not closed")
}

// What agentx will hand to the YAML parser. A SKILL.md comes from any
// repository a user adds, so its frontmatter is untrusted input, and the
// parser builds its whole tree before parsing it: a block that nests far
// enough costs memory faster than it costs bytes, and exhausting memory
// ends the process outright rather than returning an error agentx could
// report. These bounds are orders of magnitude above every published skill
// measured, whose blocks run to a few hundred bytes and open at most a
// handful of collections.
const (
	maxBlockBytes = 64 << 10
	maxFlowOpens  = 256
)

// bounded refuses a block agentx will not parse, so that the reason reaches
// the user as a warning and the skill is named after its directory, rather
// than the parser taking the process down with it.
func bounded(block string) error {
	if len(block) > maxBlockBytes {
		return fmt.Errorf("unparsable frontmatter: the --- block is %d bytes, more than the %d agentx reads", len(block), maxBlockBytes)
	}
	if opens := strings.Count(block, "[") + strings.Count(block, "{"); opens > maxFlowOpens {
		return fmt.Errorf("unparsable frontmatter: the --- block opens %d lists or mappings, more than the %d agentx reads", opens, maxFlowOpens)
	}
	return nil
}

// scalar renders one frontmatter value. A string is taken as it is, a number
// or a boolean written without quotes is rendered as it was read, and a
// mapping, a sequence or a missing key contributes nothing.
func scalar(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case bool, int, int64, uint64, float32, float64:
		return fmt.Sprint(v)
	}
	return ""
}

// yamlPosition is the [line:column] a go-yaml error starts with.
var yamlPosition = regexp.MustCompile(`^\[(\d+):\d+\]\s*`)

// unparsable turns a go-yaml error into the one line the scan warns with: the
// line counted from the start of the file, since the block starts on the
// second one, and the reason without the library's own coordinates and
// source excerpt.
func unparsable(err error) error {
	reason, _, _ := strings.Cut(err.Error(), "\n")
	reason = strings.TrimSpace(reason)
	if m := yamlPosition.FindStringSubmatch(reason); m != nil {
		if line, convErr := strconv.Atoi(m[1]); convErr == nil {
			return fmt.Errorf("unparsable frontmatter at line %d: %s", line+1, clip(reason[len(m[0]):]))
		}
	}
	return fmt.Errorf("unparsable frontmatter: %s", clip(reason))
}

// clip makes a go-yaml message fit in a warning. The library quotes the
// text it choked on, which comes from the file and so can be any length and
// carry anything, including the control characters that drive a terminal.
func clip(reason string) string {
	var b strings.Builder
	for _, r := range reason {
		if b.Len() >= 120 {
			b.WriteString("…")
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// SkillFrontmatter reads the name and description of a SKILL.md. An
// unusable frontmatter is an error and both are empty.
func SkillFrontmatter(text string) (name, description string, err error) {
	fm, err := parseFrontmatter(text)
	return fm.name, fm.description, err
}
