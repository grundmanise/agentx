---
status: accepted
date: 2026-11-18
---

# Serve is the backend of the desktop app and of the frontend in a browser

Today the desktop app reaches the machine two ways ([ADR 0001](0001-go-cli-owns-machine-and-account-tauri-renders.md)): one `agentx` process per action, whose NDJSON and exit code the Rust shell reads, and one long-lived `agentx serve` child for the inventory stream and the `refresh` and `search` requests. Only the Rust shell can start those processes, so the frontend works only inside the Tauri window. It cannot run in a browser against the CLI on this machine, so a change to the frontend can be checked against a real machine only by building the whole app.

`agentx serve` becomes the backend of this machine for every graphical client. It speaks JSON-RPC 2.0, and the desktop app and the same frontend in a browser send it the same requests. The terminal CLI is unchanged: every command runs in its own process, as today.

## The protocol

JSON-RPC 2.0, the standard for a request, its response and one-way notifications. On standard input and output each message is one line, which is JSON-RPC over newline-delimited JSON, as the stdio transport of MCP is. On the WebSocket each message is one text frame. Serve invents no envelope of its own.

| Method | Kind | What it does |
| --- | --- | --- |
| `run` | request | Runs one agentx command line, `agentx <args> --json`. An optional deadline stops it. The result carries the exit code. |
| `event` | notification from serve | One event of a run, with the id of its request, or one event serve emits on its own (inventory, drift, reconcile, update), with none. The event is the object the CLI writes today, with the same `type`, fields and `schema_version`. |
| `$/cancelRequest` | notification to serve | Stops the request whose id it names, by the name the Language Server Protocol gives it. |
| `refresh`, `search` | request | Today's stdin requests. Their results carry what the `refresh_complete` and `search` events carry today. |

A command that ends with an exit code other than 0 is a result, since the command ran: the error of JSON-RPC is kept for a request serve could not carry out, such as an unknown method or a command that could not start, with the standard codes.

The events are unchanged inside the notification, so the app's generated event types and the code that reads them stay as they are.

## How serve runs a command

Serve runs each `run` as a child process of its own: the same `agentx` executable, in a process group of its own, with serve's environment and working directory. It forwards the child's lines as `event` notifications, and on `$/cancelRequest` or a passed deadline sends the group SIGTERM, then SIGKILL 2 seconds later. This is what the Rust shell does today, moved into Go where every client can use it.

So a command behaves exactly as it does now. A crash in a command ends that command alone, a cancel ends every git the command started, and no state is shared between two commands.

Serve does not scan for a command it ran. Two jobs are kept apart. Serve keeps the inventory equal to the machine: it scans when the machine changes and sends the inventory that results. Whether a client needs the inventory to hold what its own command changed before it goes on is for the client to say, since only the client knows what it shows and what it does next. It says so with `refresh`, sent after a command that may have changed something, as the desktop app does today. So `run` carries a command and its outcome and nothing else, both wires follow one rule, and neither the CLI nor serve keeps a list of the commands that change nothing.

Running a command inside the serve process, with no process start, is a later step and is decided per command by measurement. It is not free: the commands that finish a merge change the process's working directory, git calls read it, a panic in a goroutine a command started would end serve, and a command that ignores its context could not be stopped. A command moves inside serve only when a measured start-up cost justifies it and those four are handled for it. No client changes when one does: the request and its events stay the same.

## The doors

| Door | Who uses it | Who may connect |
| --- | --- | --- |
| Standard input and output, one message per line | The desktop app, which starts serve as its child, as today | Only the parent process |
| A WebSocket on `127.0.0.1`, opened only with `agentx serve --listen`, one message per frame | The frontend in a browser on this machine | A client that presents the token serve made for this run, and whose `Origin` is one serve was started with |

`--listen` takes an address on the loopback interface, port `0` by default, and refuses any other address. Serve reports the address and the token in a `listening` event. The token is random for each serve process and is never written to a log. `--origin` names each web origin that may connect, such as the frontend's development server; with none, no browser may. Serve also refuses a request whose `Host` is not the address it listens on, so that a web page cannot reach it through a name that resolves to this machine.

The WebSocket door takes no environment and no working directory from a request: a command it runs gets serve's own. The environment decides which programs git starts, so a client that could set it could run any program.

Serve does not serve the frontend. In development the frontend's own server does, which is what a change to it is checked on. One command, `pnpm dev:real` in `apps/desktop`, starts the development server and `agentx serve --listen` with that server's origin, reads the `listening` event, and has the development server hand the address and token to the page, so that opening a browser is all that is left to do.

One serve runs per agentx home, so `agentx serve --listen` exits with code 6 while the desktop app is open. A debug build of the app starts its own serve with `--listen` when asked to, and the browser then shares it.

A door that listens beyond this machine, for a phone, is not part of this decision. It needs pairing, a scope that only reads by default, transport security, a way to serve the frontend, and a layout for a small screen. It also works only while this machine is awake and reachable, so whether it is worth building is weighed against a web app that reads the snapshot on the account remote, and it gets an ADR of its own.

## The frontend

The frontend keeps the transport module it has, with the same functions: a run with its lines and its result, cancel, and the serve child's events. Under it are two ways to send: through the Rust shell inside the Tauri window, and through the WebSocket in a browser. The screens, the store and the end-to-end tests, which run against a mocked Rust shell, do not change.

What only the window can do is replaced or hidden in a browser: the app's preferences are kept in the browser's storage, a link opens in a new tab, and showing a file in Finder, opening an editor and the native open dialog are not offered.

The desktop app moves onto serve's standard input and output after that, in a change of its own: the Rust shell then keeps one child and a pipe, and stops running one-shot processes. Until then the app works as it does today, so the browser door adds to the product and changes nothing in it.

## The terminal CLI

Every command keeps running in its own process, whether a serve runs or not. It is complete on its own, it prompts on its own terminal, and it uses the environment of the shell it was started from. Serve learns of its changes as today, from the mutation counter and the file watches.

## Why

The frontend can be checked in a browser against the CLI of this machine without building the Rust shell, by a person or by an end-to-end test, with one command.

One backend serves every graphical client. The browser door is the same serve the app uses, with the same requests, so a later client is one more door and never a second backend.

The risk stays where it can be seen. Commands run as processes, as they do today, so the first step changes how a client reaches a command and nothing about what a command does. The speed-up of running inside serve is taken command by command, where it is measured.

## Considered and rejected

A separate command that relays the Rust shell's messages to a browser: a second backend beside serve, with a protocol only it speaks, and nothing a later client or a faster serve could build on.

Running every command inside serve from the first step: faster for each click, but it changes what a crash, a cancel and a working directory mean for every command at once, before any of it is measured.

The terminal CLI as a client of a running serve: the CLI on `PATH` and the serve the app started can be of different releases, so a command would run other code than the binary the user typed. Serve has no terminal, so the confirmation of `import`, a passphrase prompt of git or ssh, colours and the terminal's width would each need relaying. The environment a command needs is the terminal's, with its agent and credential helpers. The lock and the mutation counter already let a terminal and the app work at once.

Request lines of our own, as `refresh` and `search` are today, with a run and a cancel request added: fewer changes, but a protocol only agentx documents. Newline-delimited JSON says how messages are framed and nothing about requests, ids, errors or cancelling, so each of those would be ours to define and to explain to whoever writes a client.

Serve scanning after every command it ran, with the result naming the inventory that holds the change: a client would need no rule of its own, but a command that only reads would pay for a scan, and the choice of when data must be fresh would sit in the transport, away from the client that makes it.

The CLI marking the commands that change nothing, for serve to skip the scan after them and for the app to read as a generated list: no list is written by hand, but the CLI and serve would carry a fact that only a client uses, and a mark for each command cannot tell a run that previews, as `adopt` with no flag does, from one that writes.

gRPC, or a schema compiler beside Go: a second toolchain for the same events, and nothing a browser speaks without a proxy.

## Consequences

The [CLI contract](../spec/cli-contract.md) gains the protocol, the methods, `--listen`, `--origin` and the `listening` event. Its section on serve's stdin requests becomes the `refresh` and `search` methods, and serve's own events become `event` notifications; the request lines of today stay accepted until the desktop app has moved. The [inventory-ordering contract](../spec/inventory-ordering.md) gains that a client of the door sends `refresh` after its own commands, as the desktop app does.

The desktop app stops keeping a list of the commands that change nothing: the code that runs a command says whether it reads or changes state, and a refresh follows the second kind.

The end-to-end tests keep the mocked CLI. A small set of tests runs the frontend in a browser against a real `agentx serve --listen`, one happy path per screen.

The work lands in order, each step usable on its own: the protocol, `run` and the WebSocket door in serve; the browser's way to send and `pnpm dev:real`; the desktop app on serve's standard input and output; then commands inside serve, one at a time, where measured.
