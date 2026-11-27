---
name: screenshots
description: "Use when taking screenshots or recordings of the desktop app, such as visual evidence for a PR."
---

Take screenshots and recordings in browser mode: the app runs in a browser against the real `agentx serve`, inside a container, on a home built from a machine preset. Never run the app, serve or a preset on the host, even on a Mac.

## Run it

From `apps/desktop`, with Docker running (in a cloud session, start it with `dockerd > /tmp/dockerd.log 2>&1 &` if `docker info` fails):

```sh
container/browser up typical       # once per change: builds the image from this worktree and starts the app
container/browser exec agent-browser skills get core
container/browser exec agent-browser open http://localhost:1431
container/browser pull             # copies /tmp/out to .browser/ in the worktree
container/browser down
```

- Read agent-browser's own guide first (`skills get core`) and drive the app with its commands. The app is at http://localhost:1431 inside the container.
- Pick the preset that shows the change: `typical` for most screens, `empty` for a first launch, `heavy` for long lists, `hostile` for unusual names. Each `up` starts from a fresh home.
- Change the state with the app itself, or with `container/browser exec agentx ...`. Never write agentx's own files by hand.
- Write screenshots and recordings under `/tmp/out`.
- agent-browser 0.44 places clicks at the wrong point while the viewport's pixel density is 2: click at density 1, and raise it to 2 only for a screenshot.
- For a before and after, run `up` in a worktree of the base commit, take the shot, then `up` on the branch.

Use the mocked e2e tests (`e2e/harness/cli.ts`) only for a state the real serve cannot reach, such as a failed launch check.
