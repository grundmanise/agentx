package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExitCodesMatchContract(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "spec", "cli-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		contains(t, "docs/spec/cli-contract.md", string(doc), fmt.Sprintf("| %d | `%s` |", s.exit, s.code))
	}
}
