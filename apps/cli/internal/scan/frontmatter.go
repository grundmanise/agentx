package scan

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

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
			return fmt.Errorf("unparsable frontmatter at line %d: %s", line+1, reason[len(m[0]):])
		}
	}
	return fmt.Errorf("unparsable frontmatter: %s", reason)
}

// SkillFrontmatter reads the name and description of a SKILL.md. An
// unusable frontmatter is an error and both are empty.
func SkillFrontmatter(text string) (name, description string, err error) {
	fm, err := parseFrontmatter(text)
	return fm.name, fm.description, err
}
