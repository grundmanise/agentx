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
	exitInterrupted  = status{9, "interrupted"}
	exitInternal     = status{10, "internal"}
)

var statuses = []status{
	exitOK, exitUsage, exitGit, exitSource, exitPendingMerge,
	exitNotFound, exitRefused, exitLocked, exitAccountRepo, exitInterrupted,
	exitInternal,
}

// interruptedFailure is how a run that a stop signal ended answers. It is
// its own code and not a refusal: the desktop app cancels the runs it
// starts, and a cancel it asked for may not read as a collision it did not.
// It stands in for whatever the run was about to report, which is why the
// hint sends the reader to doctor rather than naming one thing: doctor
// names every kind of state a stop can leave – an unfinished journal, a
// remote no source names, the staging refs of an install – each with its
// own repair, and a stop usually leaves none of them.
func interruptedFailure() error {
	return fail(exitInterrupted, "interrupted",
		"run the command again; 'agentx doctor' names anything the stop left behind")
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
	return refuse(st, message, hint)
}

// refuse is fail for a caller that holds on to the failure rather than
// returning it at once, such as a batch collecting what it could not do.
func refuse(st status, message, hint string) *failure {
	return &failure{status: st, message: message, hint: hint}
}
