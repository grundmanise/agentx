package cli

import "github.com/grundmanise/agentx/apps/cli/internal/scan"

// readLibrary is the one way this package reads the library, so what a run
// costs the library is one thing a test can count. A read lists every
// directory the library holds and content-hashes each of them: work that
// belongs to the library and not to the skills the run is about, and work
// that grows with the library rather than with the run. A command
// therefore reads the library a fixed number of times, whatever the number
// of skills it touches, and reading it once per skill is the mistake the
// cost test names.
var readLibrary = scan.ReadLibrary
