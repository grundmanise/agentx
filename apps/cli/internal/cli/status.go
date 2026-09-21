package cli

// status is one row of the exit code table: the process exit code and the
// string carried as `code` in an error event. The table is documented in
// docs/spec/cli-contract.md; a test keeps the two in step.
type status struct {
	exit int
	code string
}

var (
	exitOK           = status{0, "ok"}
	exitUsage        = status{1, "usage"}
	exitGit          = status{2, "git"}
	exitSource       = status{3, "source"}
	exitPendingMerge = status{4, "pending_merge"}
	exitNotFound     = status{5, "not_found"}
	exitRefused      = status{6, "refused"}
	exitLocked       = status{7, "locked"}
	exitAccountRepo  = status{8, "account_repo"}
	exitInternal     = status{10, "internal"}
)

var statuses = []status{
	exitOK, exitUsage, exitGit, exitSource, exitPendingMerge,
	exitNotFound, exitRefused, exitLocked, exitAccountRepo, exitInternal,
}

// failure is the error a command returns to end with a non-zero exit code.
// Any other error is reported as internal.
type failure struct {
	status  status
	message string
	hint    string
	cause   error // what a caller matches with errors.Is, when it has to
}

func (f *failure) Error() string { return f.message }
func (f *failure) Unwrap() error { return f.cause }

// wrap records the cause a caller may match on and returns f.
func (f *failure) wrap(cause error) *failure { f.cause = cause; return f }

func fail(st status, message, hint string) error {
	return &failure{status: st, message: message, hint: hint}
}
