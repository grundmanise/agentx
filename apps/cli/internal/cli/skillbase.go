package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// managedRecord is the lineage of the managed skill a command compares with
// or puts back to its base version, read in the one for-each-ref a listing
// reads. A skill with no base version to read is refused, each for its own
// reason: an unmanaged skill has none, a fork's is decided by its own
// history, which is not this command's, and a branch whose trailers agentx
// cannot read records none it can trust. what says what the command would
// have done with the base: "compare with", "revert to".
func (inv *invocation) managedRecord(ctx context.Context, name, what string) (string, lineage.Record, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return "", lineage.Record{}, accountRepoFailure(err)
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return "", lineage.Record{}, accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
		}
	}
	rec, ok := records[name]
	switch {
	case !ok:
		return "", lineage.Record{}, fail(exitRefused, fmt.Sprintf("%s is not managed by agentx, so it has no base version to %s", name, what),
			"run 'agentx skill list' to see which skills are managed")
	case rec.Kind == lineage.KindFork:
		return "", lineage.Record{}, fail(exitRefused, name+" is a fork on this machine",
			"a fork's versions are its own history; this command works on a managed skill")
	case !rec.HasImport:
		return "", lineage.Record{}, fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref),
			"run 'agentx doctor' and check the account repo it names")
	}
	return gitDir, rec, nil
}

// readLibraryTree reads a library skill's directory as git would record it,
// refusing one this machine cannot read, as a library it cannot use is
// refused. The directory read is the real one, a library entry that is a
// symlink being read where it leads, as a scan reads it.
func (inv *invocation) readLibraryTree(lib string) (treeid.Tree, error) {
	real, err := filepath.EvalSymlinks(lib)
	if err != nil {
		return treeid.Tree{}, libraryFailure(inv.dirs.Library, err)
	}
	tree, err := treeid.Read(real)
	if err != nil {
		return treeid.Tree{}, libraryFailure(inv.dirs.Library, err)
	}
	return tree, nil
}

// baseBlobs are the object ids of what a base version's files and symlinks
// hold, the ones a command that lays the version out has to read.
func baseBlobs(base lineage.Base) []string {
	var ids []string
	for _, e := range base.Entries {
		if source.IsFileMode(e.Mode) || e.Mode == treeid.SymlinkMode {
			ids = append(ids, e.OID)
		}
	}
	return ids
}

// materialise lays a base version out under dest, which it creates: every
// directory, every file with the mode git records for it and every symlink,
// and nothing else. Every path is the account repo's own bytes, so each one
// is held to the rule a path of a source is held to before anything is
// written, as an install holds it.
func materialise(dest string, base lineage.Base, bodies map[string]string) error {
	for _, e := range base.Entries {
		if err := source.CheckPath(e.Path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, e := range base.Entries {
		full := filepath.Join(dest, filepath.FromSlash(e.Path))
		if e.Mode == source.DirMode {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return err
			}
			continue
		}
		body, ok := bodies[e.OID]
		if !ok {
			return fmt.Errorf("the account repo does not hold %s of the base version", e.Path)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		switch {
		case e.Mode == treeid.SymlinkMode:
			if err := os.Symlink(body, full); err != nil {
				return err
			}
		case source.IsFileMode(e.Mode):
			mode := os.FileMode(0o644)
			if e.Mode == source.ExecutableMode {
				mode = 0o755
			}
			if err := writeSynced(full, []byte(body), mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s of the base version is neither a file, a directory nor a symlink", e.Path)
		}
	}
	return nil
}
