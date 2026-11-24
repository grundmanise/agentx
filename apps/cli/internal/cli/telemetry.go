package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// Where usage data goes: set by the release build through -ldflags, empty in
// a source build, which then neither asks, records nor sends anything.
// AGENTX_TELEMETRY_ENDPOINT and AGENTX_TELEMETRY_KEY replace them.
var (
	telemetryHost string
	telemetryKey  string
)

// The values of the telemetry setting. Absent is unset: never answered.
const (
	telemetryOn    = "on"
	telemetryOff   = "off"
	telemetryUnset = "unset"
)

// The spool: one file per record under telemetry/ in agentx home, at most
// spoolCap of them and none older than spoolMaxAge. A flush sends at most
// flushBatch records in one request, within flushBudget. A one-shot command
// starts a flush when the last attempt is flushEvery old, and serve flushes
// that often.
const (
	spoolCap    = 500
	spoolMaxAge = 7 * 24 * time.Hour
	flushBatch  = 100
	flushBudget = 3 * time.Second
	flushEvery  = 10 * time.Minute
)

// telemetryIDKey keys the HMAC that derives the telemetry id from the
// machine id. It differs from the machine id's own key, so the telemetry
// id cannot be joined with the machine id without the machine id itself.
const telemetryIDKey = "agentx-telemetry-id/v1"

// The types of spool record and the event each is sent as. A new record
// type is one line here; the flush reads nothing else of it.
const (
	recordCommand    = "command"
	recordServeStart = "serve_start"
)

var telemetryEvents = map[string]string{
	recordCommand:    "command_run",
	recordServeStart: "serve_start",
}

// telemetryID derives the telemetry id from the machine id.
func telemetryID(machineID string) string {
	mac := hmac.New(sha256.New, []byte(telemetryIDKey))
	mac.Write([]byte(machineID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// telemetryEndpoint is the host usage data goes to: AGENTX_TELEMETRY_ENDPOINT,
// else the build's, else none.
func (inv *invocation) telemetryEndpoint() string {
	if v := inv.env["AGENTX_TELEMETRY_ENDPOINT"]; v != "" {
		return v
	}
	return telemetryHost
}

// telemetryAPIKey is the project key sent with each batch.
func (inv *invocation) telemetryAPIKey() string {
	if v := inv.env["AGENTX_TELEMETRY_KEY"]; v != "" {
		return v
	}
	if telemetryKey != "" {
		return telemetryKey
	}
	return "none"
}

// doNotTrack reports DO_NOT_TRACK set to anything but 0 or false.
func (inv *invocation) doNotTrack() bool {
	switch strings.ToLower(strings.TrimSpace(inv.env["DO_NOT_TRACK"])) {
	case "", "0", "false":
		return false
	}
	return true
}

// telemetryOffered reports whether this run may ask for consent at all: a
// host is known and DO_NOT_TRACK is not set.
func (inv *invocation) telemetryOffered() bool {
	return inv.telemetryEndpoint() != "" && !inv.doNotTrack()
}

// recordingTelemetry reports whether usage data is recorded and sent: the
// setting is on, a host is known and DO_NOT_TRACK is not set. It reads the
// settings; an unreadable file records nothing.
func (inv *invocation) recordingTelemetry() bool {
	if !inv.telemetryOffered() {
		return false
	}
	s, err := home.LoadSettings(inv.dirs.Home)
	return err == nil && s.Telemetry == telemetryOn
}

// askConsent is whether a run offers telemetry: offered, and the setting unset.
func (inv *invocation) askConsent() bool {
	if !inv.telemetryOffered() {
		return false
	}
	s, err := home.LoadSettings(inv.dirs.Home)
	return err == nil && s.Telemetry == ""
}

// interactive reports whether a person is there to answer: stdin and stdout
// are terminals, or AGENTX_INTERACTIVE says so.
func (inv *invocation) interactive(in io.Reader) bool {
	switch inv.env["AGENTX_INTERACTIVE"] {
	case "1":
		return true
	case "0":
		return false
	}
	return isTerminal(in) && isTerminal(inv.out.stdout)
}

// telemetryStatement is what the terminal shows before it asks.
const telemetryStatement = `agentx can send anonymous usage data to help fix what fails. For each command
it sends the command's name without arguments, its exit code and duration,
whether --json was given, the agentx version, the OS and CPU type, and an id
derived from the machine id that cannot be traced back to it. Crash reports,
with paths removed, fall under the same answer. It never sends a path, a skill
or server name, a URL or anything you typed.
Details: https://docs.agentx.wtf/telemetry
Change your answer at any time with: agentx config set telemetry on|off`

// offerTelemetry asks a person at a terminal once whether to send usage
// data, and stores the answer: on for y or yes, off for anything else.
// A run that cannot ask leaves the setting unset for a later run.
func (inv *invocation) offerTelemetry(ctx context.Context, in io.Reader) error {
	if inv.out.json || !inv.askConsent() || !inv.interactive(in) {
		return nil
	}
	inv.out.print()
	inv.out.print(telemetryStatement)
	inv.out.prompt("Send anonymous usage data? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	value := telemetryOff
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		value = telemetryOn
	}
	if err := inv.setTelemetry(ctx, value); err != nil {
		return err
	}
	inv.out.done("telemetry is now " + inv.out.paint(heading, value))
	return nil
}

// setTelemetry stores the setting; turning it off drops what is spooled.
func (inv *invocation) setTelemetry(ctx context.Context, value string) error {
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		s.Telemetry = value
		return home.SaveSettings(inv.dirs.Home, s)
	})
	if err == nil && value == telemetryOff {
		err = os.RemoveAll(spoolDir(inv.dirs.Home))
	}
	return err
}

// settingsTelemetryID is the telemetry id the settings event carries: set
// only while the setting is on and DO_NOT_TRACK is unset, which counts as
// off for the desktop app too.
func (inv *invocation) settingsTelemetryID(s home.Settings) string {
	if s.Telemetry != telemetryOn || inv.doNotTrack() {
		return ""
	}
	id, err := inv.telemetryIDNow(context.Background())
	if err != nil {
		return ""
	}
	return id
}

// telemetryIDNow is this machine's telemetry id, storing a machine id
// first when there is none yet.
func (inv *invocation) telemetryIDNow(ctx context.Context) (string, error) {
	id, err := home.KnownMachineID(inv.dirs.Home, inv.env)
	if err == nil && id == "" {
		id, _, err = home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	}
	if err != nil {
		return "", err
	}
	return telemetryID(id), nil
}

func spoolDir(dir string) string { return filepath.Join(dir, "telemetry") }

// lastFlushPath marks the last flush attempt by its modification time.
func lastFlushPath(dir string) string { return filepath.Join(spoolDir(dir), "last-flush") }

// spoolRecord writes one record of type typ to the spool when telemetry is
// recording: the envelope every record carries, then fields, which may not
// replace it. Recording never fails a command, so neither does this: a
// record that cannot be written is dropped.
func (inv *invocation) spoolRecord(ctx context.Context, typ string, fields map[string]any) {
	if inv.command == "" || !inv.recordingTelemetry() {
		return
	}
	id, err := inv.telemetryIDNow(ctx)
	if err != nil {
		return
	}
	rec := map[string]any{}
	for k, v := range fields {
		rec[k] = v
	}
	rec["type"] = typ
	rec["telemetry_id"] = id
	rec["cli_version"] = cliVersion
	rec["schema_version"] = schemaVersion
	rec["os"] = runtime.GOOS
	rec["arch"] = runtime.GOARCH
	rec["command"] = inv.command
	rec["at"] = time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = writeSpool(spoolDir(inv.dirs.Home), b)
}

// writeSpool writes one record under a name that sorts by time, through a
// temporary file so a flush never reads half of it, then trims the spool.
func writeSpool(dir string, b []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	name := fmt.Sprintf("%020d-%s.json", time.Now().UnixNano(), hex.EncodeToString(r[:]))
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	trimSpool(dir, time.Now())
	return nil
}

// spooled lists the records of the spool, oldest first.
func spooled(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}

// spooledAt is when a record was written, read from its name.
func spooledAt(name string) (time.Time, bool) {
	stamp, _, ok := strings.Cut(name, "-")
	if !ok {
		return time.Time{}, false
	}
	ns, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, ns), true
}

// trimSpool drops the records older than spoolMaxAge or with a name that
// tells no time, then the oldest beyond spoolCap, and returns the rest.
func trimSpool(dir string, now time.Time) []string {
	var kept []string
	for _, name := range spooled(dir) {
		if at, ok := spooledAt(name); !ok || now.Sub(at) > spoolMaxAge {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		kept = append(kept, name)
	}
	for len(kept) > spoolCap {
		_ = os.Remove(filepath.Join(dir, kept[0]))
		kept = kept[1:]
	}
	return kept
}

// startFlush sends the spool after a command: in a detached agentx
// telemetry flush that the command does not wait for, or in-process with
// AGENTX_TELEMETRY_FLUSH=inline. It does nothing while telemetry is not
// recording, the spool is empty or the last attempt is under flushEvery old.
func (inv *invocation) startFlush(ctx context.Context) {
	if inv.command == "" || inv.command == flushCommand || !inv.recordingTelemetry() {
		return
	}
	dir := spoolDir(inv.dirs.Home)
	if len(spooled(dir)) == 0 {
		return
	}
	if inv.env["AGENTX_TELEMETRY_FLUSH"] == "inline" {
		inv.flush(context.WithoutCancel(ctx))
		return
	}
	if info, err := os.Stat(lastFlushPath(inv.dirs.Home)); err == nil && time.Since(info.ModTime()) < flushEvery {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "telemetry", "flush")
	for k, v := range inv.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // its own session: a Ctrl-C to the command's group misses it
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// flushCommand is the hidden command that sends the spool; its own run is
// never recorded.
const flushCommand = "telemetry flush"

func newTelemetryCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{Use: "telemetry", Hidden: true, Args: cobra.NoArgs}
	cmd.AddCommand(&cobra.Command{
		Use:    "flush",
		Short:  "Send the spooled usage data",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inv.recordingTelemetry() {
				inv.flush(cmd.Context())
			}
			return nil
		},
	})
	return cmd
}

// batchEntry is one event of a batch.
type batchEntry struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Timestamp  string         `json:"timestamp"`
	Properties map[string]any `json:"properties"`
}

// batchEntryOf builds the entry of one spool record, false for a record
// that is not one: unreadable, of an unknown type or without its id.
func batchEntryOf(b []byte) (batchEntry, bool) {
	var rec map[string]any
	if json.Unmarshal(b, &rec) != nil {
		return batchEntry{}, false
	}
	typ, _ := rec["type"].(string)
	event, known := telemetryEvents[typ]
	id, _ := rec["telemetry_id"].(string)
	at, _ := rec["at"].(string)
	if !known || id == "" || at == "" {
		return batchEntry{}, false
	}
	props := map[string]any{}
	for k, v := range rec {
		switch k {
		case "type", "telemetry_id", "at":
		default:
			props[k] = v
		}
	}
	props["$process_person_profile"] = false
	props["$lib"] = "agentx-cli"
	props["$lib_version"] = rec["cli_version"]
	return batchEntry{Event: event, DistinctID: id, Timestamp: at, Properties: props}, true
}

// flush posts up to flushBatch spooled records in one request within
// flushBudget. A 2xx or 4xx answer deletes the records it carried; any
// other answer, or none, keeps them for the next flush. It takes no lock:
// a record written meanwhile waits for the next one.
func (inv *invocation) flush(ctx context.Context) {
	dir := spoolDir(inv.dirs.Home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	now := time.Now()
	_ = os.WriteFile(lastFlushPath(inv.dirs.Home), nil, 0o600)
	_ = os.Chtimes(lastFlushPath(inv.dirs.Home), now, now)
	names := trimSpool(dir, now)
	var sent []string
	batch := []batchEntry{}
	for _, name := range names {
		if len(batch) == flushBatch {
			break
		}
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		entry, ok := batchEntryOf(b)
		if !ok {
			_ = os.Remove(path)
			continue
		}
		batch = append(batch, entry)
		sent = append(sent, path)
	}
	if len(batch) == 0 {
		return
	}
	body, err := json.Marshal(struct {
		APIKey string       `json:"api_key"`
		Batch  []batchEntry `json:"batch"`
	}{inv.telemetryAPIKey(), batch})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, flushBudget)
	defer cancel()
	url := strings.TrimSuffix(inv.telemetryEndpoint(), "/") + "/batch/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agentx/"+cliVersion)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		inv.out.debugf("telemetry: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	_ = res.Body.Close()
	if ok := res.StatusCode >= 200 && res.StatusCode < 300 || res.StatusCode >= 400 && res.StatusCode < 500; !ok {
		inv.out.debugf("telemetry: the endpoint answered %d", res.StatusCode)
		return
	}
	for _, path := range sent {
		_ = os.Remove(path)
	}
}

// serveTelemetryJob flushes the spool at start and every flushEvery while
// serve runs, off the loop's goroutine so it never delays a scan.
func (inv *invocation) serveTelemetryJob(ctx context.Context) func() {
	for {
		if inv.recordingTelemetry() {
			inv.flush(ctx)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(flushEvery):
		}
	}
}
