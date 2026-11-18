---
status: accepted
date: 2026-11-18
---

# Serve is the backend, and every client calls it over JSON-RPC

Today the desktop app reaches the machine two ways ([ADR 0001](0001-go-cli-owns-machine-and-account-tauri-renders.md)): one `agentx` process per action, whose NDJSON and exit code the Rust shell reads, and one long-lived `agentx serve` child for the inventory stream and the `refresh` and `search` requests. Only the Rust shell can start those processes, so the frontend works only inside the Tauri window. It cannot run in a browser against the CLI on this machine, and a phone cannot reach it at all.

`agentx serve` becomes the backend of this machine. It holds the methods that read and change the machine, and every client calls them the same way, over JSON-RPC 2.0: the desktop app, the same frontend in a browser, and later a phone. The terminal CLI stays complete on its own: it calls the same methods, through a running serve when there is one and in its own process when there is none.

## The protocol

JSON-RPC 2.0, one JSON object per message. A request names a method and its params and carries an id. While it runs, serve sends notifications tagged with that id: the events the command writes today, with the same `type`, fields and `schema_version`, so the app's generated event types and the code that reads them stay as they are. The request ends with one response: its result, or an error with the same exit code, message and hint the command reports today. The inventory, drift, reconcile and update events serve emits on its own are notifications with no request id.

Requests that exist from the start:

| Method | What it does |
| --- | --- |
| `command.run` | Runs one agentx command line inside serve, as `agentx <args> --json` would run in a process of its own, with the working directory and environment the request names. Its events arrive as notifications; the response carries the exit code. |
| `cancel` | Cancels a request by its id. |
| `refresh`, `search` | Today's stdin requests, as methods. |

`command.run` lets every client use every command on day one, through the code each command already has: `Run` in `apps/cli/internal/cli` takes its arguments, environment and streams as parameters and reads nothing from the process. Typed methods, such as `skill.add` with typed params, then replace it one command at a time, generated into TypeScript from one schema as the event and command types are today.

## The doors

Serve answers on three doors. All three speak the same protocol.

| Door | Who uses it | Who may connect |
| --- | --- | --- |
| Standard input and output | The desktop app, which starts serve as its child, as today | Only the parent process |
| A local socket, `serve.sock` in agentx home | The terminal CLI and other programs on this machine | The user who owns agentx home: the socket is created with mode 0600 |
| A WebSocket on `127.0.0.1`, opened only with `agentx serve --listen` | The frontend in a browser on this machine | A client that presents the token serve made for this run and whose `Origin` and `Host` headers name the address serve listens on |

`--listen` takes an address on the loopback interface, port `0` by default, and serve reports the URL and token in a `listening` event. The token is random for each serve process and is never written to a log. A door that listens beyond this machine, for a phone, is not part of this decision: it needs pairing, a read-only scope by default and transport security, and gets an ADR of its own.

The frontend gets one JSON-RPC client. Inside the Tauri window it sends messages through the Rust shell to serve's standard input; in a browser it opens the WebSocket. The Rust shell keeps one child and routes replies by id; it no longer runs one-shot processes, sets deadlines per process or kills process groups. The few things only the window can do, showing a file in Finder, opening an editor and the native open dialog, stay in the Rust shell and are hidden or replaced in a browser.

## The terminal CLI

`agentx <command>` first tries `serve.sock`. When a serve answers, the CLI sends `command.run` with its arguments, its working directory and its environment, prints the notifications as it would print its own events, and exits with the code the response carries. When no serve answers, it runs the command in its own process, exactly as today. The output, the exit codes and the files written are the same both ways, so a script cannot tell them apart, and the CLI never needs a daemon. `agentx serve` itself, `doctor` and `version` always run in their own process.

## How serve runs a request

- **One queue for changes.** Commands that change agentx home run one at a time, in the order received. Reads, scans and the update check run beside them. Every command still takes the advisory locks and writes its mutation journal, because a CLI that runs in its own process, or a crash, can still meet it.
- **A panic ends the request, not serve.** Serve recovers it and answers the request with an error. The app restarts serve only when the process itself ends.
- **Cancel through the context.** `cancel` cancels the request's context. Every git and every other process a command starts ends with that context, the unattended ones included.
- **The working directory is a parameter.** The places that read the process's working directory read the request's instead.
- **After a change, the response names the inventory that holds it**, so a client no longer sends `refresh` after its own commands. The refresh rules of the [inventory-ordering contract](../spec/inventory-ordering.md) remain for changes made outside serve.

## Why

The frontend in a browser, the desktop app and the terminal then see one machine through one door. The frontend can be checked in a browser against the CLI of this machine without building the Rust shell. A phone can be added later as one more client, with no second backend to build.

The app gets faster and simpler. A click no longer starts a process, which costs most on macOS; the inventory and the source index stay in memory; and a command from the app can no longer find the lock held by serve and exit 7, because serve runs both. The Rust shell shrinks to one child and a pipe.

The terminal keeps what ADR 0001 promised. Every command still runs without a GUI and without a daemon, and a terminal user still gets exactly what an app user gets, now through the same code path whenever serve runs.

## Considered and rejected

A separate command that relays the Rust shell's messages to a browser: a second backend beside serve, and it cannot run while the app runs, because only one serve may run per agentx home.

gRPC, or a schema compiler beside Go: a second toolchain for the same events. JSON-RPC 2.0 carries the NDJSON events as they are, and LSP and MCP show that it works for long requests with progress.

Making the terminal CLI always need a running serve: a script or CI job would have to start a daemon first, and ADR 0001 keeps the CLI complete on its own.

Running every command in its own process forever: it keeps the browser and phone out, and keeps the cost of a process start on every click.

## Consequences

Serve becomes long-lived state that must not leak. Caches are keyed by ref and commit, as the source index already is, and a soak test runs serve under load for hours.

Every method must honour cancel through its context, and every package-level variable a command writes must become per request.

The [CLI contract](../spec/cli-contract.md) gains the protocol, the methods, the `listening` event and `serve.sock`; its section on serve's stdin requests becomes the `refresh` and `search` methods. The inventory-ordering contract's "Refresh after a command" shrinks to changes made outside serve. The desktop app's transport and its Rust supervisor are rewritten around one client.

The work lands in order, each step usable on its own: the protocol and `command.run` on standard input and output, with the app moved onto it; the WebSocket door with `--listen`; the CLI as a client of `serve.sock`; typed methods, one command at a time.
