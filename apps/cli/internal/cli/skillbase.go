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
// reads, see managedRefusal.
func (inv *invocation) managedRecord(ctx context.Context, name, what string) (string, lineage.Record, error) {
	gitDir, rec, ok, err := inv.accountRecord(ctx, name)
	if err != nil {
		return "", lineage.Record{}, err
	}
	if err := managedRefusal(name, what, rec, ok); err != nil {
		return "", lineage.Record{}, err
	}
	return gitDir, rec, nil
}

// accountRecord is the branch the account repo holds for the skill called
// name, read in the one for-each-ref a listing reads, and false when it
// holds none, as an account repo that does not exist yet holds none. A
// command that works on managed skills and forks alike reads it first, to
// tell which of the two it was given.
func (inv *invocation) accountRecord(ctx context.Context, name string) (string, lineage.Record, bool, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return "", lineage.Record{}, false, accountRepoFailure(err)
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return "", lineage.Record{}, false, accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
		}
	}
	rec, ok := records[name]
	return gitDir, rec, ok, nil
}

// managedRefusal refuses a skill with no base version to read, rec being
// its branch and ok whether it has one, each for its own reason: an
// unmanaged skill has none, a fork's is decided by its own history, which
// is not this command's, and a branch whose trailers agentx cannot read
// records none it can trust. what says what the command would have done
// with the base: "compare with", "revert to".
func managedRefusal(name, what string, rec lineage.Record, ok bool) error {
	switch {
	case !ok:
		return fail(exitRefused, fmt.Sprintf("%s is not managed by agentx, so it has no base version to %s", name, what),
			"run 'agentx skill list' to see which skills are managed")
	case rec.Kind == lineage.KindFork:
		return fail(exitRefused, name+" is a fork on this machine",
			"a fork's versions are its own history; this command works on a managed skill")
	case !rec.HasImport:
		return fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref),
			"run 'agentx doctor' and check the account repo it names")
	}
	return nil
}

// notAForkRefusal refuses a flag that names one of a fork's commits, given
// for the skill called name, which has no commits of its own: a managed
// skill, ok being whether the account repo holds its branch, whose one
// version is its base, or an unmanaged one. what says what the flag would have done
// with the commit: "compare with", "revert to"; and verb is the command
// that works on the base instead.
func notAForkRefusal(name, what, verb string, ok bool) error {
	if ok {
		return fail(exitRefused, sanitised(name)+" is managed, not a fork, so it has no commit of its own to "+what,
			"run '"+skillCommand(verb, name)+"' to "+what+" its base version")
	}
	return fail(exitRefused, sanitised(name)+" is not a fork, so it has no commit to "+what,
		"fork it first with '"+skillCommand("fork", name)+"'")
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
		if source.IsFileMode(e.Mode) || e.Mode == source.SymlinkMode {
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
		case e.Mode == source.SymlinkMode:
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
