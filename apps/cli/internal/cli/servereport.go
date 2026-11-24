package cli

import (
	"bytes"
	"context"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// The serve child writes this machine's inventory report to the hub on its
// own while the machine setting report_inventory is on and a hub is set
// (ADR 0018): after a scan, once the machine has been quiet for
// reportQuietFor, so that a run of changes makes one commit. It is the
// report agentx machine report writes, built from the inventory serve
// scanned, and pushed only when the hub's branch does not hold it.

// scannedInventory is one inventory of the serve child as a report is built
// from it: whether its scan set out to list the library, and why it could
// not, nil when it did.
type scannedInventory struct {
	inventory scan.Inventory
	listed    bool
	unlisted  *failure
}

// reportsInventory reports whether this machine writes its inventory report
// on its own: the machine setting report_inventory is on, and a hub is set
// to write it to. It reads the settings and runs no git.
func (inv *invocation) reportsInventory() (bool, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return false, err
	}
	_, hub := hubEntry(s)
	return s.ReportInventory && hub, nil
}

// scan is one scan of the serve child, see serveScan, kept for the report.
// While this machine reports its inventory the library is listed whatever
// the output mode, since the report lists it; otherwise it is listed for
// an inventory emitted as JSON alone, as agentx scan lists it. The setting
// is read before each scan, so that turning it on leads to an inventory a
// report can be built from: the write of the settings sets off the scan.
func (c *serveChecks) scan(ctx context.Context) (scan.Inventory, error) {
	inv := c.inv
	library := libraryEmitted
	if reports, err := inv.reportsInventory(); err == nil && reports {
		library = libraryListed
	}
	inventory, unlisted, err := inv.serveScan(ctx, library)
	if err == nil {
		c.scanned.Store(&scannedInventory{inventory: inventory, listed: library == libraryListed || inv.out.json, unlisted: unlisted})
	}
	return inventory, err
}

// writtenReport is what one report of the serve child left on the hub: the
// document, the branch it was written to, the branch's tip now, whether a
// commit was pushed and, after a push, the read of the hub's machines.
type writtenReport struct {
	data     []byte
	branch   reportBranch
	commit   string
	pushed   bool
	machines fleetRead
}

// report is the quiet job of the serve child, which serve runs once the
// machine has been quiet for reportQuietFor after a scan, and never beside
// itself or a check. With the machine setting report_inventory off, or no
// hub set, it runs no git and reports nothing, and what the last reports
// came to is forgotten, see forgetReports. Otherwise it writes this
// machine's report from the last inventory scanned, see writeScanned, and
// its outcome is reported on the loop's goroutine, see reportWrite. The
// setting is read on every run. A hub check runs it too while the reports
// fail, see serveChecks.hub.
func (c *serveChecks) report(ctx context.Context) func() {
	reports, err := c.inv.reportsInventory()
	if err == nil && !reports {
		return c.forgetReports
	}
	// An inventory whose library is not listed was scanned before this
	// machine reported: the scan that turning the setting on, or setting a
	// hub, sets off brings one that lists it.
	sc := c.scanned.Load()
	if err != nil || sc == nil || !sc.listed {
		return nil
	}
	w, err := c.writeScanned(ctx, sc)
	if err == nil && w.data == nil {
		return nil
	}
	return func() { c.reportWrite(w, err) }
}

// forgetReports forgets what the last reports came to, on the loop's
// goroutine, once this machine no longer reports: when it reports again,
// the branch is read again and a report that fails warns.
func (c *serveChecks) forgetReports() {
	c.reportFailing, c.reported = false, nil
}

// reportWrite reports what one report came to. One that was pushed is a
// machine_report event, then the fleet event, since the fleet changed. One
// the branch held already is nothing. A failure is a warning prefixed
// "machine report: " when the reports start failing, and none while they go
// on failing, see serveChecks.reportFailing: the report is tried again
// after the next scan, and by the next hub check that reaches the hub. What
// the branch holds after a failure is not known, so the next report reads
// it.
func (c *serveChecks) reportWrite(w writtenReport, err error) {
	out := c.inv.out
	if err != nil {
		if !c.reportFailing {
			out.warn("machine report: " + err.Error())
		}
		c.reportFailing, c.reported = true, nil
		return
	}
	c.reportFailing, c.reported = false, w.data
	if !w.pushed {
		return
	}
	out.emit(machineReportEvent{event: newEvent("machine_report"), Machine: w.branch.id, Commit: w.commit, Pushed: true})
	out.print(out.paint(heading, "report"), ": ", w.branch.written(w.commit))
	if w.machines.err != nil {
		out.warn("machine report: " + w.machines.err.Error())
	}
	c.reportFleet(w.machines)
}

// writeScanned writes the report of the inventory sc to the hub, as agentx
// machine report writes one, outside the lock: the document is built, the
// machine's branch is read, see readReportBranch, and the document is
// pushed when the branch does not hold it, see writeReport. A document
// that is the one the branch held after the last report is written
// nowhere and no git is run for it: nothing was written then, and data is
// nil. An inventory whose library could not be listed writes no report
// and is a failure, as for the command. After a push, the hub's machines
// are read again from the store's remote-tracking refs, which the push
// moved, with no call to the hub.
func (c *serveChecks) writeScanned(ctx context.Context, sc *scannedInventory) (writtenReport, error) {
	inv := c.inv
	if sc.unlisted != nil {
		return writtenReport{}, noReportUnlisted(sc.unlisted)
	}
	data, err := reportDocument(sc.inventory)
	if err != nil || bytes.Equal(data, c.reported) {
		return writtenReport{}, err
	}
	b, err := inv.readReportBranch(ctx, sc.inventory.Machine.ID)
	if err != nil {
		return writtenReport{}, err
	}
	w := writtenReport{data: data, branch: b}
	if w.commit, w.pushed, err = inv.writeReport(ctx, b, data); err != nil {
		return writtenReport{}, err
	}
	if w.pushed {
		held, err := inv.git.TrackedBranches(ctx, b.gitDir, b.remote, machineBranches()...)
		if err != nil {
			w.machines = fleetRead{err: storeFailure(err)}
		} else {
			w.machines = c.fleetHeld(ctx, b.gitDir, b.remote, b.url, held)
		}
	}
	return w, nil
}
