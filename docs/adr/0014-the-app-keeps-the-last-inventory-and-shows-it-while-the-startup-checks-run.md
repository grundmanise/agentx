---
status: accepted
date: 2026-11-14
---

# The desktop app keeps the last inventory it applied and shows it while the startup checks run

A launch runs four steps before the app knows the machine: it reads the login shell's environment, runs `agentx version` and `agentx doctor`, then starts `agentx serve` and waits for its first inventory. Each step starts processes, and on macOS that takes long enough to see. Until now the main panel stayed empty for all of it.

The app now keeps the last inventory it applied, with the slices it loads once the checks pass (the machine settings, the machine, the sources and the adoption preview), as the **kept inventory**. A launch that finds one fills its store from it before the first frame, so the screens show at once. The checks run as before, at the same time as the user reads. Until they pass, the screen in the main panel is washed out and pulses slowly, and nothing else says that they run. The title bar and the sidebar are as always, and the user can go to any screen and select anything in it.

The kept inventory is shown, never trusted:

- **No command runs against it.** A command a control starts before the checks pass waits for them, and the control shows as pending. Cancel stops a command that waits, and it never starts. A read waits for the checks however long they take.
- **A change runs only on the machine the user saw.** A command names a skill, and the CLI looks the name up when it runs. So a command that changes something runs after the checks only if the serve child's first inventory says of the machine what the kept one said: otherwise the name may now be another skill's, and the command is dropped. The screen then shows the machine as it is, and the user can start the command again. Such a command does not outlive a failed check either: the startup layer has replaced the control that started it, so it does not run after a Retry.
- **It is not applied.** It has no serve child, instance or scan counter, so the first inventory of the first serve child replaces it whole, by the [inventory-ordering contract](../spec/inventory-ordering.md). The slices are replaced by the reads that follow the checks.
- **A failed check hides it.** The startup layer takes the window, as on any launch.

The browser's `localStorage` holds it, as one item. The read is synchronous, so the kept inventory is in the first frame; a file in the app's config directory, read through the Rust side, would arrive after it. The item names the build that wrote it, and a launch drops what another build kept, since its events may have another shape. It is written only after the checks pass, from what the app read from the CLI in that run.

`version` and `doctor` now run at the same time: both only read the machine, and neither needs the other's result. Doctor still gates serve, since serve's first scan creates agentx home. A launch with no kept inventory, such as the first one, shows a spinner and "Loading" from its first frame; before, a spinner and the name of the running check showed only after 2 seconds.

This amends [ADR 0001](0001-go-cli-owns-machine-and-account-tauri-renders.md): the app keeps a copy of what the CLI last told it. It still reads nothing of the machine itself. It leaves [ADR 0004](0004-no-local-database-git-holds-lineage-files-hold-machine-state.md) as it is: the kept inventory holds no state. Deleting it changes one thing, what the next launch shows before its checks pass.

## Considered and rejected

Caching in the CLI, with `serve` emitting a stored inventory before its first scan: it would show nothing sooner, since the login shell, `version` and `doctor` all run before serve starts, and it would give the CLI a second source of an inventory beside a scan. Starting serve before doctor passes: serve's first scan writes to agentx home, which doctor checks first. Disabling every control while the checks run: the user could not select a skill or change screen, and each screen would need its own disabled state. A banner or a status line that says the checks run: the wash and its pulse already say that what is shown is loading, without a text to read on every launch. The wash with no pulse: a still, washed-out screen reads as disabled, not as loading. Naming the running check on a launch with no kept inventory: which check runs is of no use to the user until one fails, and the startup layer names it then.

## Consequences

For a moment after launch, the app can show a machine that has changed since: a skill removed in the terminal is listed until the first inventory arrives. The wash and its pulse are what say so. A read a screen starts in that time, such as a skill's files, shows as loading until the checks pass.

The kept inventory holds what an inventory holds, the paths and names on this machine among them, in the webview's storage for the app's origin. The webview loads only the app's bundled code, under the content security policy in `tauri.conf.json`.

An inventory too large for the storage's quota is not kept, and what was kept before is removed, so that the next launch shows the empty panel and its spinner, never an older machine.
