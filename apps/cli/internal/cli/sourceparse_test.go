package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestParseSourceNeverEchoesACredential covers every refusal of a source
// argument that names what it was given: a token in the URL must not reach
// the message or the hint a command prints, whichever way the input is
// wrong. Each of these is refused before anything else runs, so the refusal
// parseSource returns is the whole of what source add prints for it.
func TestParseSourceNeverEchoesACredential(t *testing.T) {
	t.Parallel()
	const secret = "ghp_S3CRETT0KEN"
	for _, arg := range []string{
		"https://user:" + secret + "@github.com",
		"https://" + secret + "@github.com/owner",
		"https://user:" + secret + "@host:notaport/owner/repo",
		"https://user:" + secret + "@github.com/owner/repo/blob/main/SKILL.md",
		"https://user:" + secret + "@github.com/owner/repo#",
		"https://user:" + secret + "@github.com/owner/../repo",
		"ftp://user:" + secret + "@example.com/repo",
		"user:" + secret + "@example.com/owner/repo",
		"https://u:p#" + secret + "@github.com/owner/repo",
	} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			inv := &invocation{out: &writer{stdout: &stdout, stderr: &stderr, env: map[string]string{}}}
			_, err := inv.parseSource(arg)
			var f *failure
			if !errors.As(err, &f) || f.status != exitUsage {
				t.Fatalf("parseSource(%q) = %v, want a usage refusal", arg, err)
			}
			if printed := f.message + f.hint + stdout.String() + stderr.String(); strings.Contains(printed, secret) {
				t.Errorf("the token reached the refusal: %q", printed)
			}
			// A message either leaves the input out or names it redacted;
			// what it may never do is repeat the userinfo it was given.
			if strings.Contains(f.message, "@") {
				contains(t, "message", f.message, "***@")
			}
		})
	}
}
