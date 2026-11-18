---
status: accepted
date: 2026-11-18
---

# Serve is the backend of the desktop app and of the frontend in a browser

Today the desktop app reaches the machine two ways ([ADR 0001](0001-go-cli-owns-machine-and-account-tauri-renders.md)): one `agentx` process per action, whose NDJSON and exit code the Rust shell reads, and one long-lived `agentx serve` child for the inventory stream and the `refresh` and `search` requests. Only the Rust shell can start those processes, so the frontend works only inside the Tauri window. It cannot run in a browser against the CLI on this machine, so a change to the frontend can be checked against a real machine only by building the whole app.

`agentx serve` becomes the backend of this machine for every graphical client. It takes requests and answers with events, as it does today for `refresh` and `search`, and the desktop app and the same frontend in a browser send it the same requests. The terminal CLI is unchanged: every command runs in its own process, as today.

## The protocol

The protocol is the one serve has, with two more requests. A request is one JSON object with a `type` and a `request_id` the client chose, and serve answers with events that carry that `request_id`. Events keep the `type`, fields and `schema_version` they have today, so the app's generated event types and the code that reads them stay as they are.

| Request | What it does |
| --- | --- |
| `run` | Runs one agentx command line, `agentx <args> --json`. Each line the command writes comes back as a `run_line` event that holds it, and a `run_end` event closes the request with the exit code. An optional deadline stops the run. |
| `cancel` | Stops the run of the `request_id` it names. |
| `refresh`, `search` | As today. |

The inventory, drift, reconcile and update events serve emits on its own carry no `request_id`, as today.

## How serve runs a command

Serve runs each `run` as a child process of its own: the same `agentx` executable, in a process group of its own, with serve's environment and working directory. It forwards the child's lines as `run_line` events, and on `cancel` or a passed deadline sends the group SIGTERM, then SIGKILL 2 seconds later. This is what the Rust shell does today, moved into Go where every client can use it.

So a command behaves exactly as it does now. A crash in a command ends that command alone, a cancel ends every git the command started, and no state is shared between two commands. When a command that may have changed something ends, serve scans, and `run_end` names the inventory that holds the change, so a client no longer sends `refresh` after its own commands.

Running a command inside the serve process, with no process start, is a later step and is decided per command by measurement. It is not free: the commands that finish a merge change the process's working directory, git calls read it, a panic in a goroutine a command started would end serve, and a command that ignores its context could not be stopped. A command moves inside serve only when a measured start-up cost justifies it and those four are handled for it. No client changes when one does: the request and its events stay the same.

## The doors

| Door | Who uses it | Who may connect |
| --- | --- | --- |
| Standard input and output, one JSON object per line | The desktop app, which starts serve as its child, as today | Only the parent process |
| A WebSocket on `127.0.0.1`, opened only with `agentx serve --listen`, one JSON object per message | The frontend in a browser on this machine | A client that presents the token serve made for this run, and whose `Origin` is one serve was started with |

`--listen` takes an address on the loopback interface, port `0` by default, and refuses any other address. Serve reports the address and the token in a `listening` event. The token is random for each serve process and is never written to a log. `--origin` names each web origin that may connect, such as the frontend's development server; with none, no browser may. Serve also refuses a request whose `Host` is not the address it listens on, so that a web page cannot reach it through a name that resolves to this machine.

The WebSocket door takes no environment and no working directory from a request: a command it runs gets serve's own. The environment decides which programs git starts, so a client that could set it could run any program.

Serve does not serve the frontend. In development the frontend's own server does, which is what a change to it is checked on. One command, `pnpm dev:browser` in `apps/desktop`, starts `agentx serve --listen` with that server's origin, reads the `listening` event, and starts the development server with the address and token, so that opening a browser is all that is left to do.

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

JSON-RPC 2.0, or gRPC: a second message format beside the events every command already writes. Serve's own requests already pair a request with its events by `request_id`, which is all a run needs, and the generated types cover them. A standard envelope would matter for clients other people write, and can be added as one more door if that day comes.

## Consequences

The [CLI contract](../spec/cli-contract.md) gains the `run` and `cancel` requests, the `run_line`, `run_end` and `listening` events, `--listen` and `--origin`. The `refresh` and `search` requests stay as they are. The [inventory-ordering contract](../spec/inventory-ordering.md) gains that serve scans after a command it ran.

The end-to-end tests keep the mocked CLI. A small set of tests runs the frontend in a browser against a real `agentx serve --listen`, one happy path per screen.

The work lands in order, each step usable on its own: the `run` and `cancel` requests and the WebSocket door in serve; the browser's way to send and `pnpm dev:browser`; the desktop app on serve's standard input and output; then commands inside serve, one at a time, where measured.
