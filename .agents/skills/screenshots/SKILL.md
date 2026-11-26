---
name: screenshots
description: "Use when taking screenshots or recordings of the desktop app, such as visual evidence for a PR."
---

Take screenshots and recordings in browser mode: the app runs in a browser against the real `agentx serve`, inside a container, on a home built from a machine preset. Never run the app, serve or a preset on the host, even on a Mac.

## Run it

From `apps/desktop`, with Docker running (in a cloud session, start it with `dockerd > /tmp/dockerd.log 2>&1 &` if `docker info` fails):

```sh
container/browser up typical          # once per change: builds the image from this worktree and starts the app
container/browser ab open http://localhost:1431
container/browser ab set viewport 1440 900
container/browser ab snapshot -i      # the interactive elements as refs, such as @e3
container/browser ab click @e3
container/browser shot after.png     # saved at 2x to /tmp/out
container/browser pull                # copies /tmp/out to .browser/ in the worktree
container/browser down
```

- Pick the preset that shows the change: `typical` for most screens, `empty` for a first launch, `heavy` for long lists, `hostile` for unusual names. Each `up` starts from a fresh home.
- Change the state with the app itself, or with `container/browser shell` and real `agentx` commands. Never write agentx's own files by hand.
- Keep the viewport at pixel density 1 and take screenshots with `shot`: agent-browser places clicks at the wrong point at density 2.
- Write every other file under `/tmp/out`. Recordings: `ab record start /tmp/out/flow.webm`, then `ab record stop`.
- For a before and after, run `up` in a worktree of the base commit, take the shot, then `up` on the branch.
- `container/browser ab --help` lists every agent-browser command.

Use the mocked e2e tests (`e2e/harness/cli.ts`) only for a state the real serve cannot reach, such as a failed launch check.
