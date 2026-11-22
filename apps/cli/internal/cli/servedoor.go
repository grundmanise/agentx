package cli

import (
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/proc"
	"github.com/grundmanise/agentx/apps/cli/internal/rpc"
)

// listeningEvent says where the WebSocket door of a serve started with
// --listen is, and the token a client presents to it. It is written to
// stdout alone: the token is never sent through the door and never logged.
type listeningEvent struct {
	event
	URL   string `json:"url"`   // what a client connects to: ws://<host>:<port>/, the port being the one that was free when none was asked for
	Token string `json:"token"` // random for each serve process
}

const (
	listenHint = "pass a loopback address, such as 127.0.0.1, localhost:4319 or [::1]"
	originHint = "pass a web origin as a browser sends it, such as http://localhost:1420, with nothing after the host and the port"
)

// listenAddress is the address --listen names, as net.Listen takes it: a
// loopback address, 127.0.0.1 for localhost, with the port after it, 0
// when none is given, which is any free port. Any other address is
// refused: the door is for this machine alone.
func listenAddress(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		host, port = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), "0"
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("--listen %s is not a loopback address", value)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("--listen %s has no valid port", value)
	}
	return net.JoinHostPort(ip.String(), port), nil
}

// webOrigin is nil for an origin as a browser sends it in the Origin
// header: scheme://host, with the port when it is not the scheme's own,
// in lower case and with nothing after it. Origins are compared whole, so
// one written any other way would match no browser.
func webOrigin(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Scheme+"://"+u.Host != value || strings.ToLower(value) != value {
		return fmt.Errorf("--origin %s is not a web origin", value)
	}
	return nil
}

// serveDoor is the WebSocket door of one serve process: the JSON-RPC
// server and the listener it answers on.
type serveDoor struct {
	rpc  *rpc.Server
	http *http.Server
}

// openDoor listens on address, an address listenAddress returned, reports
// where with a listening event, and only then starts to send every event
// serve emits on its own through the door as well, so the token goes to
// stdout alone. The inventory is retained: it is the first thing a client
// that connects later is sent. Its methods are those of serve's stdin.
func (inv *invocation) openDoor(address string, origins []string, methods map[string]rpc.Method) (*serveDoor, error) {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fail(exitRefused, "cannot listen on "+address+": "+err.Error(), "pass another port, or none for one that is free")
	}
	token := rand.Text()
	at := l.Addr().(*net.TCPAddr)
	bound := at.String()
	d := &serveDoor{}
	d.rpc = rpc.New(rpc.Options{
		Token:   token,
		Hosts:   []string{bound, net.JoinHostPort("localhost", strconv.Itoa(at.Port))},
		Origins: origins,
		Methods: methods,
	})
	d.http = &http.Server{Handler: d.rpc, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = d.http.Serve(l) }() // ends when close closes the listener

	ev := listeningEvent{event: newEvent("listening"), URL: "ws://" + bound + "/", Token: token}
	inv.out.emit(ev)
	inv.out.print(inv.out.paint(heading, "listening"), ": ", ev.URL)
	inv.out.print(inv.out.paint(heading, "token"), ": ", ev.Token)
	inv.out.tee = d.own
	return d, nil
}

// close stops the door: no client connects any more, every run in
// progress is stopped as a cancel stops it and waited for, so that no
// command serve started outlives it, and every connection is closed.
func (d *serveDoor) close() {
	_ = d.http.Close()
	d.rpc.Close()
}

// own sends one event serve emitted on its own to every client, a log
// event as one of stderr.
func (d *serveDoor) own(ev any) {
	stream := proc.Stdout
	if _, ok := ev.(logEvent); ok {
		stream = proc.Stderr
	}
	_, inventory := ev.(inventoryEvent)
	d.rpc.Notify("event", eventParams{Stream: stream, Event: ev}, inventory)
}
