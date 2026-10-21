package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// eventTypesFile is where the desktop app reads the types of the events
// from. It is generated from the structs this package marshals, so that a
// change to an event reaches the app's type check: TestEventTypes fails
// until the file is written again, and the file written again fails the
// app's check wherever the app reads what changed.
const eventTypesFile = "../../../desktop/src/cli/events.gen.ts"

// eventTypes names the struct each event type is marshalled from, in the
// order the contract describes them.
var eventTypes = []struct {
	name string
	v    any
}{
	{"version", versionEvent{}},
	{"settings", settingsEvent{}},
	{"machine", machineEvent{}},
	{"source", sourceEvent{}},
	{"source_skill", sourceSkillEvent{}},
	{"progress", progressEvent{}},
	{"library_skill", librarySkillEvent{}},
	{"diff", diffEvent{}},
	{"history", historyEvent{}},
	{"fork_note", forkNoteEvent{}},
	{"update_available", updateAvailableEvent{}},
	{"conflict", conflictEvent{}},
	{"export", exportEvent{}},
	{"import_skill", importSkillEvent{}},
	{"pull", pullEvent{}},
	{"publish", publishEvent{}},
	{"installable_skill", installableSkillEvent{}},
	{"error", errorEvent{}},
	{"result", resultEvent{}},
	{"log", logEvent{}},
	{"snapshot", snapshotEvent{}},
	{"doctor", doctorEvent{}},
	{"adoption", adoptionEvent{}},
	{"reconcile", reconcileEvent{}},
	{"drift", driftEvent{}},
	{"refresh_complete", refreshCompleteEvent{}},
	{"search", searchEvent{}},
}

// reservedEventTypes are event types the contract reserves and no command
// emits. The app knows them and renders nothing for them.
var reservedEventTypes = []string{"operation", "fleet"}

// fieldRef names one field of a struct by its Go name.
type fieldRef struct {
	in    any
	field string
}

// fieldKey is a fieldRef as a map key: in may hold a slice, which no map
// key can.
type fieldKey struct {
	t     reflect.Type
	field string
}

func (f fieldRef) key() fieldKey { return fieldKey{reflect.TypeOf(f.in), f.field} }

// typeOverride gives the TypeScript type of one field.
type typeOverride struct {
	fieldRef
	ts string
}

// valueList is a string field whose values the contract lists. It becomes a
// union of string literals, named name, so that a value the app compares a
// field with is one the CLI writes. An open list is one the contract says
// will gain more values, which the app shows as written: its union admits
// any string beside the ones it names.
type valueList struct {
	name   string
	values []string
	open   bool
	fields []fieldRef
}

func errorCodes() []string {
	var codes []string
	for _, s := range statuses {
		if s != exitOK {
			codes = append(codes, s.code)
		}
	}
	return codes
}

// valueLists are the value lists of the events, each from the constants the
// CLI writes.
var valueLists = []valueList{
	{name: "SkillKind", values: []string{lineage.KindManaged, lineage.KindUnmanaged}, fields: []fieldRef{
		{scan.LibraryEntry{}, "Kind"}, {reconcileEvent{}, "Kind"}, {driftEvent{}, "Kind"},
		{updateAvailableEvent{}, "Kind"}, {conflictEvent{}, "Kind"}, {exportSkill{}, "Kind"},
	}},
	{name: "SkillState", values: []string{stateCurrent, stateModified}, fields: []fieldRef{
		{scan.LibraryEntry{}, "State"}, {driftEvent{}, "State"}, {driftEvent{}, "PreviousState"},
	}},
	{name: "DriftState", open: true, values: []string{
		driftDisplaced, driftMissing, driftSourceRemoved, driftUpstreamRemoved, driftNotPublished,
	}, fields: []fieldRef{
		{scan.LibraryEntry{}, "Drift"}, {driftEvent{}, "Drift"}, {driftEvent{}, "PreviousDrift"},
	}},
	{name: "PlacementMode", values: []string{modeSymlink, modeCopy, modeLibrary}, fields: []fieldRef{
		{scan.LibraryPlacement{}, "Mode"},
	}},
	{name: "PlacementKind", values: []string{scan.KindSymlink, scan.KindCopy, scan.KindDirectory, modeLibrary}, fields: []fieldRef{
		{scan.LibraryPlacement{}, "Kind"},
	}},
	{name: "OccurrenceKind", values: []string{scan.KindSymlink, scan.KindCopy, scan.KindDirectory}, fields: []fieldRef{
		{scan.Occurrence{}, "Kind"},
	}},
	{name: "OccurrenceScope", values: []string{scan.ScopeUser, scan.ScopeProject}, fields: []fieldRef{
		{scan.Occurrence{}, "Scope"},
	}},
	{name: "Transport", values: []string{mcp.TransportStdio, mcp.TransportSSE, mcp.TransportStreamableHTTP}, fields: []fieldRef{
		{scan.ServerOccurrence{}, "Transport"},
	}},
	{name: "ToolKind", values: []string{mcp.KindTool, mcp.KindPrompt, mcp.KindResource, mcp.KindResourceTemplate}, fields: []fieldRef{
		{home.Tool{}, "Kind"},
	}},
	{name: "HandshakeReason", values: []string{
		mcp.ReasonUnauthorized, mcp.ReasonUnreachable, mcp.ReasonNotFound, mcp.ReasonTimeout, mcp.ReasonFailed,
	}, fields: []fieldRef{
		{scan.HandshakeError{}, "Reason"},
	}},
	{name: "FileStatus", values: []string{diffAdded, diffModified, diffDeleted}, fields: []fieldRef{
		{diffEvent{}, "Status"}, {changedFile{}, "Status"},
	}},
	{name: "DoctorStatus", values: []string{rowOK, rowWarn, rowFail, rowInfo}, fields: []fieldRef{
		{doctorEvent{}, "Status"},
	}},
	{name: "LogLevel", open: true, values: []string{levelWarn, levelDebug}, fields: []fieldRef{
		{logEvent{}, "Level"},
	}},
	{name: "ErrorCode", values: errorCodes(), fields: []fieldRef{
		{errorEvent{}, "Code"},
	}},
	{name: "PublishOutcome", values: []string{
		publishPushed, publishUpToDate, publishBehind, publishMoved, publishDeclined, publishDenied, publishRefused,
	}, fields: []fieldRef{
		{publishEvent{}, "Outcome"},
	}},
	{name: "PullOutcome", values: []string{pullFastForward, pullMerged, pullConflict, pullRefused}, fields: []fieldRef{
		{pullEvent{}, "Outcome"},
	}},
	{name: "ReconcileOutcome", values: []string{
		outcomeRestored, outcomeRepaired, outcomeWorktreeMissing, outcomeAdoptCandidate,
		outcomeModified, outcomeInstallable, outcomeUnmanaged,
	}, fields: []fieldRef{
		{reconcileEvent{}, "Outcome"},
	}},
	{name: "AdoptionState", values: []string{adoptCandidate, adoptAdopted, adoptManaged, adoptRefused}, fields: []fieldRef{
		{adoptionEvent{}, "State"},
	}},
	{name: "ImportState", values: []string{restorePresent, restoreMissing, restoreDifferent}, fields: []fieldRef{
		{importSkillEvent{}, "State"},
	}},
	{name: "MachineDerivation", values: []string{home.DerivedPlatform, home.DerivedRandom}, fields: []fieldRef{
		{machineEvent{}, "Derivation"},
	}},
	{name: "SourceAccess", values: []string{home.AccessWritable, home.AccessReadOnly, home.AccessUnknown}, fields: []fieldRef{
		{sourceEvent{}, "Access"}, {home.Source{}, "Access"},
	}},
	{name: "ProgressPhase", values: []string{
		phaseFetch, phasePublish, phaseBlobs, phaseImport, phaseInstall, phasePlace, phaseAdopt, phaseRescan,
	}, fields: []fieldRef{
		{progressEvent{}, "Phase"},
	}},
}

// typeOverrides gives the TypeScript type of a field whose Go type does not
// say it: raw JSON, which the CLI writes as it was read.
var typeOverrides = []typeOverride{
	{fieldRef{home.Settings{}, "CopyMode"}, "Record<string, string[]>"},
}

// typeNames names the structs whose Go name would read wrongly in the app or
// collide with another.
var typeNames = map[reflect.Type]string{
	reflect.TypeFor[scan.Skill]():   "SkillNode",
	reflect.TypeFor[scan.Server]():  "McpServer",
	reflect.TypeFor[home.Tool]():    "McpTool",
	reflect.TypeFor[home.Source]():  "SettingsSource",
	reflect.TypeFor[source.Match](): "SearchResult",
}

// tsGen writes TypeScript declarations for Go structs by the rules
// encoding/json marshals them by: an embedded struct's fields are the outer
// struct's, an omitempty field is optional, a pointer without omitempty can
// be null, an unexported field and a "-" field are left out.
type tsGen struct {
	lists     map[fieldKey]string // the value list type of a field
	overrides map[fieldKey]string
	names     map[reflect.Type]string
	envelope  reflect.Type // the struct every event embeds
	structs   []reflect.Type
	named     map[string]reflect.Type
	used      map[fieldKey]bool // the lists and overrides a field took its type from
	nullable  map[string]bool   // the JSON names of the fields that can be null
}

func newTSGen(lists []valueList, overrides []typeOverride, names map[reflect.Type]string) *tsGen {
	g := &tsGen{lists: map[fieldKey]string{}, overrides: map[fieldKey]string{}, names: names,
		envelope: reflect.TypeFor[event](), named: map[string]reflect.Type{},
		used: map[fieldKey]bool{}, nullable: map[string]bool{}}
	for _, l := range lists {
		for _, f := range l.fields {
			g.lists[f.key()] = l.name
		}
	}
	for _, o := range overrides {
		g.overrides[o.key()] = o.ts
	}
	return g
}

// structName is the TypeScript name of a struct, recording the struct so
// that its declaration is written too.
func (g *tsGen) structName(t reflect.Type) (string, error) {
	name, ok := g.names[t]
	if !ok {
		name = strings.ToUpper(t.Name()[:1]) + t.Name()[1:]
	}
	if other, ok := g.named[name]; ok {
		if other != t {
			return "", fmt.Errorf("%s and %s are both %s in TypeScript: name one in typeNames", other, t, name)
		}
		return name, nil
	}
	g.named[name] = t
	g.structs = append(g.structs, t)
	return name, nil
}

// typeOf is the TypeScript type of a value of Go type t in field f of
// struct in.
func (g *tsGen) typeOf(in reflect.Type, f reflect.StructField, t reflect.Type) (string, error) {
	k := fieldKey{in, f.Name}
	if ts, ok := g.overrides[k]; ok {
		g.used[k] = true
		return ts, nil
	}
	if list, ok := g.lists[k]; ok {
		g.used[k] = true
		switch {
		case t.Kind() == reflect.String:
			return list, nil
		case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String:
			return list + "[]", nil
		}
		return "", fmt.Errorf("%s.%s: a value list needs a string or a slice of strings, not %s", in, f.Name, t)
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return "", fmt.Errorf("%s.%s is raw JSON: give its type in typeOverrides", in, f.Name)
	}
	switch t.Kind() {
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Slice, reflect.Array:
		elem, err := g.typeOf(in, f, t.Elem())
		return elem + "[]", err
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return "", fmt.Errorf("%s.%s: a map key must be a string, not %s", in, f.Name, t.Key())
		}
		elem, err := g.typeOf(in, f, t.Elem())
		return "Record<string, " + elem + ">", err
	case reflect.Struct:
		return g.structName(t)
	}
	return "", fmt.Errorf("%s.%s: no TypeScript type for %s", in, f.Name, t)
}

// members is the declaration body of struct t: the interfaces it extends,
// one per embedded struct but the envelope, which embeds says it has, and
// one line per field.
func (g *tsGen) members(t reflect.Type) (extends []string, envelope bool, lines []string, err error) {
	for _, f := range reflect.VisibleFields(t) {
		if len(f.Index) > 1 {
			continue // promoted from an embedded struct, which declares it
		}
		tag, hasTag := f.Tag.Lookup("json")
		if f.Anonymous {
			if hasTag {
				return nil, false, nil, fmt.Errorf("%s: embedded %s carries a json tag", t, f.Type)
			}
			if f.Type == g.envelope {
				envelope = true
				continue
			}
			if f.Type.Kind() != reflect.Struct {
				return nil, false, nil, fmt.Errorf("%s: embedded %s is not a struct", t, f.Type)
			}
			name, err := g.structName(f.Type)
			if err != nil {
				return nil, false, nil, err
			}
			extends = append(extends, name)
			continue
		}
		if !f.IsExported() || tag == "-" {
			continue
		}
		if !hasTag {
			return nil, false, nil, fmt.Errorf("%s.%s has no json tag", t, f.Name)
		}
		name, opts, _ := strings.Cut(tag, ",")
		optional := false
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitempty", "omitzero":
				optional = true
			case "":
			default:
				return nil, false, nil, fmt.Errorf("%s.%s: json option %q has no TypeScript form", t, f.Name, o)
			}
		}
		ft, null := f.Type, false
		if ft.Kind() == reflect.Pointer {
			ft, null = ft.Elem(), !optional
		}
		ts, err := g.typeOf(t, f, ft)
		if err != nil {
			return nil, false, nil, err
		}
		if null {
			ts += " | null"
			g.nullable[name] = true
		}
		mark := ""
		if optional {
			mark = "?"
		}
		lines = append(lines, fmt.Sprintf("  %s%s: %s;", name, mark, ts))
	}
	return extends, envelope, lines, nil
}

// iface is the declaration of interface name with the given members.
func iface(name string, extends, lines []string) string {
	head := "export interface " + name
	if len(extends) > 0 {
		head += " extends " + strings.Join(extends, ", ")
	}
	if len(lines) == 0 {
		return head + " {}\n"
	}
	return head + " {\n" + strings.Join(lines, "\n") + "\n}\n"
}

// union is the declaration of type name as the union of members, on one
// line when it fits the formatter's 80 columns and one member a line when
// it does not, as the formatter writes it.
func union(name string, members []string) string {
	line := "export type " + name + " = " + strings.Join(members, " | ") + ";"
	if len(line) <= 80 {
		return line + "\n"
	}
	return "export type " + name + " =\n  | " + strings.Join(members, "\n  | ") + ";\n"
}

func tsLiterals(values []string) []string {
	var q []string
	for _, v := range values {
		q = append(q, fmt.Sprintf("%q", v))
	}
	return q
}

// eventTypeName is the TypeScript name of an event type: refresh_complete
// is RefreshCompleteEvent.
func eventTypeName(typ string) string {
	var b strings.Builder
	for _, w := range strings.Split(typ, "_") {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String() + "Event"
}

// renderEventTypes is the content of eventTypesFile.
func renderEventTypes() (string, error) {
	_, d, err := eventTypesGen()
	if err != nil {
		return "", err
	}
	return d.render(), nil
}

// eventTypesGen is the generator of eventTypesFile once it has read every
// event, so that it knows every field that can be null, and the
// declarations it wrote. A value list or an override naming a field no
// event carries is an error: the field it meant would be a plain string.
func eventTypesGen() (*tsGen, declarations, error) {
	g := newTSGen(valueLists, typeOverrides, typeNames)
	d, err := g.declarations()
	if err != nil {
		return nil, declarations{}, err
	}
	for k, list := range g.lists {
		if !g.used[k] {
			return nil, declarations{}, fmt.Errorf("value list %s names %s.%s, which no event carries", list, k.t, k.field)
		}
	}
	for k := range g.overrides {
		if !g.used[k] {
			return nil, declarations{}, fmt.Errorf("a type override names %s.%s, which no event carries", k.t, k.field)
		}
	}
	return g, d, nil
}

// declarations are the declarations of the events and of the structs they
// carry.
type declarations struct {
	events, structs []string
}

func (g *tsGen) declarations() (declarations, error) {
	var events []string
	for _, e := range eventTypes {
		t := reflect.TypeOf(e.v)
		extends, envelope, lines, err := g.members(t)
		if err != nil {
			return declarations{}, err
		}
		if !envelope {
			return declarations{}, fmt.Errorf("%s does not embed the event envelope", t)
		}
		lines = append([]string{fmt.Sprintf("  type: %q;", e.name), "  schema_version: number;"}, lines...)
		events = append(events, iface(eventTypeName(e.name), extends, lines))
	}
	var structs []string
	for i := 0; i < len(g.structs); i++ { // members can record more structs
		t := g.structs[i]
		extends, envelope, lines, err := g.members(t)
		if err != nil {
			return declarations{}, err
		}
		if envelope {
			return declarations{}, fmt.Errorf("%s embeds the event envelope but is no event type", t)
		}
		name, _ := g.structName(t)
		structs = append(structs, iface(name, extends, lines))
	}
	return declarations{events, structs}, nil
}

// render is the content of eventTypesFile.
func (d declarations) render() string {
	var b strings.Builder
	b.WriteString(`// The types of the events the agentx CLI writes with --json, which
// docs/spec/cli-contract.md describes. Generated from the structs the CLI
// marshals: do not edit. To write it again, run in apps/cli:
//   go test ./internal/cli -run TestEventTypes -update

`)
	fmt.Fprintf(&b, "// The output schema version this app reads. An event of another version is\n// refused.\nexport const SCHEMA_VERSION = %d;\n\n", schemaVersion)
	b.WriteString("// What every event starts with.\nexport interface Envelope {\n  type: string;\n  schema_version: number;\n}\n\n")

	b.WriteString("// The values a field takes. An open list will gain more values, which the\n// app shows as written.\n")
	for _, l := range valueLists {
		members := tsLiterals(l.values)
		if l.open {
			members = append(members, "(string & {})")
		}
		b.WriteString(union(l.name, members))
	}
	b.WriteString("\n// The objects events carry.\n")
	b.WriteString(strings.Join(d.structs, "\n"))
	b.WriteString("\n// One type per event type.\n")
	b.WriteString(strings.Join(d.events, "\n"))

	b.WriteString("\n// Reserved by the contract and never emitted: known, and not rendered.\n")
	names := make([]string, 0, len(eventTypes)+len(reservedEventTypes))
	for _, e := range eventTypes {
		names = append(names, eventTypeName(e.name))
	}
	for _, r := range reservedEventTypes {
		fmt.Fprintf(&b, "export interface %s {\n  type: %q;\n  schema_version: number;\n}\n", eventTypeName(r), r)
		names = append(names, eventTypeName(r))
	}
	b.WriteString("\n// Every event type the app knows.\n")
	b.WriteString(union("CliEvent", names))
	b.WriteString(`export type CliEventType = CliEvent["type"];
export type EventOf<T extends CliEventType> = Extract<CliEvent, { type: T }>;

// The requests serve reads on its stdin, one JSON object a line.
export interface RefreshRequest {
  type: "refresh";
  request_id: string;
}
export interface SearchRequest {
  type: "search";
  request_id: string;
  query: string;
}
export type ServeRequest = RefreshRequest | SearchRequest;
`)
	return b.String()
}

// nullableFields are the JSON names of the fields the event types say can
// be null.
var nullableFields = sync.OnceValues(func() (map[string]bool, error) {
	g, _, err := eventTypesGen()
	if err != nil {
		return nil, err
	}
	return g.nullable, nil
})

// checkNulls fails t for every null in event e the event types do not
// admit. A nil slice or map marshals as null, while the types say an array
// or an object, which is what the contract says: the app would read the
// null without a check. The harness checks every event it parses, so the
// whole suite holds the types to what the CLI writes.
func checkNulls(t *testing.T, e map[string]any) {
	t.Helper()
	nullable, err := nullableFields()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, w := range v {
				if w == nil && !nullable[k] {
					t.Errorf("%s event: %s%s is null, which events.gen.ts does not admit", e["type"], path, k)
				}
				walk(path+k+".", w)
			}
		case []any:
			for i, w := range v {
				if w == nil {
					t.Errorf("%s event: %s%d is null, which events.gen.ts does not admit", e["type"], path, i)
				}
				walk(fmt.Sprintf("%s%d.", path, i), w)
			}
		}
	}
	walk("", e)
}

// TestEventTypes checks that the desktop app's event types are those the
// structs of this package give. Write them again with
// go test ./internal/cli -run TestEventTypes -update.
func TestEventTypes(t *testing.T) {
	t.Parallel()
	got, err := renderEventTypes()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(eventTypesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(eventTypesFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(eventTypesFile)
	if err != nil {
		t.Fatalf("%v; write it with go test ./internal/cli -run TestEventTypes -update", err)
	}
	if string(want) != got {
		t.Errorf("%s is not what the event structs give; write it again with go test ./internal/cli -run TestEventTypes -update", filepath.Base(eventTypesFile))
	}
}

// TestEventTypesListEveryEvent checks that eventTypes names every event type
// this package emits, and nothing else.
func TestEventTypesListEveryEvent(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	newEventCall := regexp.MustCompile(`newEvent\("([a-z_]+)"\)`)
	var emitted []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range newEventCall.FindAllStringSubmatch(string(src), -1) {
			emitted = append(emitted, m[1])
		}
	}
	slices.Sort(emitted)
	emitted = slices.Compact(emitted)
	var listed []string
	for _, e := range eventTypes {
		listed = append(listed, e.name)
	}
	slices.Sort(listed)
	if !slices.Equal(emitted, listed) {
		t.Errorf("event types emitted = %v, listed in eventTypes = %v", emitted, listed)
	}
}

func TestTSGenMembers(t *testing.T) {
	t.Parallel()
	type inner struct {
		A string `json:"a"`
	}
	type withList struct {
		Level string   `json:"level"`
		Drift []string `json:"drift,omitempty"`
	}
	type outer struct {
		event
		inner
		Name     string            `json:"name"`
		Count    int               `json:"count,omitempty"`
		Ok       bool              `json:"ok"`
		Sub      *string           `json:"sub,omitempty"`
		Base     *string           `json:"base"`
		Tags     []string          `json:"tags"`
		ByName   map[string]inner  `json:"by_name"`
		Raw      json.RawMessage   `json:"raw"`
		Skipped  string            `json:"-"`
		Children map[string][]bool `json:"children,omitempty"`
	}
	type rawOnly struct {
		Raw json.RawMessage `json:"raw"`
	}
	type untagged struct {
		Name string
	}
	type stringOpt struct {
		N int `json:"n,string"`
	}
	lists := []valueList{
		{name: "Level", values: []string{"a"}, fields: []fieldRef{{withList{}, "Level"}}},
		{name: "Drift", open: true, values: []string{"b"}, fields: []fieldRef{{withList{}, "Drift"}}},
	}
	overrides := []typeOverride{{fieldRef{outer{}, "Raw"}, "unknown"}}
	tests := []struct {
		name     string
		v        any
		extends  []string
		envelope bool
		lines    []string
		err      string
	}{
		{name: "fields", v: outer{}, extends: []string{"Inner"}, envelope: true, lines: []string{
			"  name: string;",
			"  count?: number;",
			"  ok: boolean;",
			"  sub?: string;",
			"  base: string | null;",
			"  tags: string[];",
			"  by_name: Record<string, Inner>;",
			"  raw: unknown;",
			"  children?: Record<string, boolean[]>;",
		}},
		{name: "value lists", v: withList{}, lines: []string{"  level: Level;", "  drift?: Drift[];"}},
		{name: "raw JSON without an override", v: rawOnly{}, err: "give its type in typeOverrides"},
		{name: "no json tag", v: untagged{}, err: "has no json tag"},
		{name: "a json option with no TypeScript form", v: stringOpt{}, err: `json option "string"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newTSGen(lists, overrides, nil)
			extends, envelope, lines, err := g.members(reflect.TypeOf(tt.v))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want one containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(extends, tt.extends) || envelope != tt.envelope || !slices.Equal(lines, tt.lines) {
				t.Errorf("members = %v, %v,\n%s\nwant %v, %v,\n%s", extends, envelope, strings.Join(lines, "\n"),
					tt.extends, tt.envelope, strings.Join(tt.lines, "\n"))
			}
		})
	}
}

func TestTSUnion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		members []string
		want    string
	}{
		{[]string{`"a"`, `"b"`}, "export type T = \"a\" | \"b\";\n"},
		{[]string{`"first-long-value"`, `"second-long-value"`, `"third-long-value"`, `"fourth"`},
			"export type T =\n  | \"first-long-value\"\n  | \"second-long-value\"\n  | \"third-long-value\"\n  | \"fourth\";\n"},
	}
	for _, tt := range tests {
		if got := union("T", tt.members); got != tt.want {
			t.Errorf("union(%v) =\n%s\nwant\n%s", tt.members, got, tt.want)
		}
	}
}
