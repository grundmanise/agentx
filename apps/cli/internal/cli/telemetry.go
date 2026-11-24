package cli

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/posthog/posthog-go/v2"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// Usage data goes to the agentx project on PostHog's EU cloud. The token is
// the project's public client token, which can only send events.
const (
	posthogHost  = "https://eu.i.posthog.com"
	posthogToken = "phc_xQuv9oeNiExAgBEey2FvxwbgSK3ZNyrDPVwbaugWgQHn"
)

// The values of the telemetry setting. Absent is unset: never answered.
const (
	telemetryOn    = "on"
	telemetryOff   = "off"
	telemetryUnset = "unset"
)

// anonymousIDKey keys the HMAC that derives the anonymous id from the
// machine id. It differs from the machine id's own key, so the anonymous id
// cannot be joined with the machine id without the machine id itself.
const anonymousIDKey = "agentx-anonymous-id/v1"

// The events the CLI sends.
const (
	eventCommandRun = "command_run"
	eventServeStart = "serve_start"
)

// anonymousID derives the anonymous id from the machine id.
func anonymousID(machineID string) string {
	mac := hmac.New(sha256.New, []byte(anonymousIDKey))
	mac.Write([]byte(machineID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// telemetryEndpoint is the host usage data goes to: AGENTX_TELEMETRY_ENDPOINT,
// else PostHog for a nightly or stable build. A source build has none, so
// it neither asks nor sends.
func (inv *invocation) telemetryEndpoint() string {
	if v := inv.env["AGENTX_TELEMETRY_ENDPOINT"]; v != "" {
		return v
	}
	if cliVersion == "dev" {
		return ""
	}
	return posthogHost
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

// telemetrySetting is the stored telemetry setting, "" when unset or
// unreadable, or when the run has no agentx home yet.
func (inv *invocation) telemetrySetting() string {
	if inv.dirs.Home == "" {
		return ""
	}
	s, err := home.LoadSettings(inv.dirs.Home)
	if err != nil {
		return ""
	}
	return s.Telemetry
}

// recordingTelemetry reports whether usage data is sent: offered, and the
// setting on.
func (inv *invocation) recordingTelemetry() bool {
	return inv.telemetryOffered() && inv.telemetrySetting() == telemetryOn
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
const telemetryStatement = `Help improve agentx

Send anonymous usage data to PostHog? We use it to find what breaks
and to make agentx better for you.

It may send:
  - the command name, without arguments or values
  - the exit code and how long it took
  - the app version, release channel, OS and CPU type
  - a non-reversible, hashed anonymous id
  - crash reports, with every path removed

It never sends paths, skill or server names, URLs, or anything you type.
Your IP address is not stored.
Details: https://docs.agentx.wtf/telemetry
Change it any time: agentx config set telemetry on|off`

// offerTelemetry asks a person at a terminal once whether to send usage
// data, and stores the answer: on for y or yes, off for anything else.
// A run that cannot ask leaves the setting unset for a later run.
func (inv *invocation) offerTelemetry(ctx context.Context, in io.Reader) error {
	if inv.out.json || !inv.telemetryOffered() || inv.telemetrySetting() != "" || !inv.interactive(in) {
		return nil
	}
	inv.out.print()
	inv.out.print(telemetryStatement)
	inv.out.print()
	inv.out.prompt("Send anonymous usage data? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	value := telemetryOff
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		value = telemetryOn
	}
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		s.Telemetry = value
		return home.SaveSettings(inv.dirs.Home, s)
	})
	if err != nil {
		return err
	}
	inv.out.done("telemetry is now " + inv.out.paint(heading, value))
	return nil
}

// settingsAnonymousID is the anonymous id the settings event carries: set
// only while the setting is on and DO_NOT_TRACK is unset, which counts as
// off for the desktop app too.
func (inv *invocation) settingsAnonymousID(s home.Settings) string {
	if s.Telemetry != telemetryOn || inv.doNotTrack() {
		return ""
	}
	id, err := inv.anonymousIDNow(context.Background())
	if err != nil {
		return ""
	}
	return id
}

// anonymousIDNow is this machine's anonymous id, storing a machine id
// first when there is none yet.
func (inv *invocation) anonymousIDNow(ctx context.Context) (string, error) {
	id, err := home.KnownMachineID(inv.dirs.Home, inv.env)
	if err == nil && id == "" {
		id, _, err = home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	}
	if err != nil {
		return "", err
	}
	return anonymousID(id), nil
}

// telemetryClient is the run's PostHog client and the anonymous id to send
// under, made on first use; nil while telemetry is not recording. What is
// enqueued is sent in the background, and closeTelemetry sends the rest
// when the run ends. It reads the setting on every call, so serve stops
// sending as soon as the setting is turned off.
func (inv *invocation) telemetryClient(ctx context.Context) (posthog.Client, string) {
	if !inv.recordingTelemetry() {
		return nil, ""
	}
	id, err := inv.anonymousIDNow(context.WithoutCancel(ctx))
	if err != nil {
		return nil, ""
	}
	inv.posthogMu.Lock()
	defer inv.posthogMu.Unlock()
	if inv.posthog == nil {
		client, err := posthog.NewWithConfig(posthogToken, posthog.Config{
			Endpoint: inv.telemetryEndpoint(),
			IsServer: posthog.Ptr(false),
			DefaultEventProperties: posthog.Properties{
				"release_channel": channelOf(cliVersion),
				"cli_version":     cliVersion,
				"schema_version":  schemaVersion,
				"arch":            runtime.GOARCH,
			},
			DefaultEventOptions: posthog.NewOptions().Set("process_person_profile", false),
			BeforeSend:          redactMessage,
			ShutdownTimeout:     time.Second,
			Logger:              quietLogger{},
		})
		if err != nil {
			return nil, ""
		}
		inv.posthog = client
	}
	return inv.posthog, id
}

// capture sends one event while telemetry is recording. Sending never fails
// a command, so neither does this.
func (inv *invocation) capture(ctx context.Context, event string, props posthog.Properties) {
	client, id := inv.telemetryClient(ctx)
	if client == nil {
		return
	}
	props["command"] = inv.command
	_ = client.Enqueue(posthog.Capture{DistinctId: id, Event: event, Properties: props})
}

// recordRun sends the command event of a run that ended with code.
func (inv *invocation) recordRun(ctx context.Context, code int) {
	if inv.command == "" {
		return
	}
	inv.capture(ctx, eventCommandRun, posthog.Properties{
		"exit_code":   code,
		"duration_ms": time.Since(inv.started).Milliseconds(),
	})
}

// closeTelemetry sends what is still queued, waiting at most the client's
// ShutdownTimeout, and drops the rest.
func (inv *invocation) closeTelemetry() {
	inv.posthogMu.Lock()
	defer inv.posthogMu.Unlock()
	if inv.posthog != nil {
		_ = inv.posthog.Close()
		inv.posthog = nil
	}
}

// userPath is a path under a user's home or a temporary directory, after
// anything that would make it part of a longer path or a module path.
var userPath = regexp.MustCompile(`(^|[^\w.\-/~<>])(?:/Users/|/home/|/root/|/private/|/tmp/|/var/folders/)[^\s"':]*`)

// redactPaths replaces every user path in s with <path>.
func redactPaths(s string) string { return userPath.ReplaceAllString(s, "$1<path>") }

// redactMessage is the client's BeforeSend: it removes every user path from
// what an event sends.
func redactMessage(m posthog.Message) posthog.Message {
	switch m := m.(type) {
	case posthog.Capture:
		m.Properties = redactValue(m.Properties).(posthog.Properties)
		return m
	case posthog.Exception:
		m.Properties = redactValue(m.Properties).(posthog.Properties)
		for i := range m.ExceptionList {
			item := &m.ExceptionList[i]
			item.Type = redactPaths(item.Type)
			item.Value = redactPaths(item.Value)
			if item.Stacktrace != nil {
				for j := range item.Stacktrace.Frames {
					item.Stacktrace.Frames[j].Filename = redactPaths(item.Stacktrace.Frames[j].Filename)
				}
			}
		}
		for i := range m.DebugImages {
			m.DebugImages[i].CodeFile = redactPaths(m.DebugImages[i].CodeFile)
		}
		return m
	}
	return m
}

// redactValue redacts the strings of a JSON-like value, in place for maps
// and slices.
func redactValue(v any) any {
	switch v := v.(type) {
	case string:
		return redactPaths(v)
	case posthog.Properties:
		for k, e := range v {
			v[k] = redactValue(e)
		}
	case map[string]any:
		for k, e := range v {
			v[k] = redactValue(e)
		}
	case []any:
		for i, e := range v {
			v[i] = redactValue(e)
		}
	case []string:
		for i, e := range v {
			v[i] = redactPaths(e)
		}
	}
	return v
}

// quietLogger keeps the client's own logging off the command's output.
type quietLogger struct{}

func (quietLogger) Debugf(string, ...any) {}
func (quietLogger) Logf(string, ...any)   {}
func (quietLogger) Warnf(string, ...any)  {}
func (quietLogger) Errorf(string, ...any) {}
