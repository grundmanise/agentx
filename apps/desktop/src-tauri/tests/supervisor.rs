// The supervisor against a stub agentx: a shell script that prints NDJSON lines, sleeps and exits
// with the code each case needs. It runs through a stub login shell profile, so the cases also
// show what a child gets from the user's shell.

use std::collections::HashMap;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use agentx_desktop_lib::supervisor::{
    Config, Emit, Supervisor, RUN_END, RUN_LINE, SERVE_EXIT, SERVE_LINE,
};
use serde_json::{json, Value};

const STUB: &str = r#"#!/bin/sh
case "$1" in
lines)
  echo '{"type":"version","schema_version":1,"cli_version":"1.0.0"}'
  echo 'not json'
  echo '{"type":"log","schema_version":1,"level":"warn","message":"careful"}' >&2
  echo '{"type":"result","schema_version":1,"ok":false}'
  exit 3
  ;;
hang)
  sleep 60 &
  echo "$!" > "$STUB_DIR/grandchild"
  echo '{"type":"progress","schema_version":1}'
  wait
  ;;
env)
  printf '{"type":"env","profile":"%s","args":"%s"}\n' "$AGENTX_STUB_PROFILE" "$*"
  ;;
serve)
  mode=$(cat "$STUB_DIR/serve-mode")
  printf '{"type":"snapshot","profile":"%s"}\n' "$AGENTX_STUB_PROFILE"
  echo '{"type":"log","schema_version":1,"level":"debug","message":"serve started"}' >&2
  case "$mode" in
  echo)
    while read -r line; do
      printf '{"type":"echo","request":%s}\n' "$line"
    done
    echo '{"type":"result","schema_version":1,"ok":true}'
    ;;
  crash)
    echo 'boom' >&2
    exit 1
    ;;
  locked)
    echo '{"type":"error","schema_version":1,"code":"refused","message":"another agentx serve holds serve.lock"}'
    exit 6
    ;;
  esac
  ;;
esac
"#;

/// What the supervisor emitted: when, the event name and its payload.
type Emitted = Arc<Mutex<Vec<(Instant, &'static str, Value)>>>;

/// One supervisor over the stub, in a directory of its own.
struct Fixture {
    dir: PathBuf,
    sup: Arc<Supervisor>,
    emitted: Emitted,
}

impl Fixture {
    fn new(name: &str) -> Self {
        let dir = std::env::temp_dir().join(format!("agentx-sup-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        let bin = dir.join("bin");
        let home = dir.join("home");
        fs::create_dir_all(&bin).unwrap();
        fs::create_dir_all(&home).unwrap();
        let stub = bin.join("agentx");
        fs::write(&stub, STUB).unwrap();
        fs::set_permissions(&stub, fs::Permissions::from_mode(0o755)).unwrap();
        let fixture_dir = dir.clone();
        let emitted: Emitted = Arc::new(Mutex::new(vec![]));
        let sink = emitted.clone();
        let emit: Emit = Arc::new(move |name, payload| {
            sink.lock().unwrap().push((Instant::now(), name, payload));
        });
        let base_env = HashMap::from([
            ("HOME".to_owned(), home.display().to_string()),
            ("SHELL".to_owned(), "/bin/bash".to_owned()),
            ("PATH".to_owned(), "/usr/bin:/bin".to_owned()),
            ("STUB_DIR".to_owned(), fixture_dir.display().to_string()),
        ]);
        let fixture = Fixture {
            sup: Supervisor::new(
                Config {
                    exe_dir: Some(bin),
                    base_env,
                    log_dir: dir.join("logs"),
                    app_version: "1.2.0".to_owned(),
                    debug: false,
                },
                emit,
            ),
            dir,
            emitted,
        };
        fixture.profile("from-profile");
        fixture
    }

    /// Sets the variable only the stub login shell profile exports.
    fn profile(&self, value: &str) {
        fs::write(
            self.dir.join("home/.bash_profile"),
            format!("export AGENTX_STUB_PROFILE={value}\n"),
        )
        .unwrap();
    }

    /// Makes the login shell take a second to start.
    fn slow_profile(&self) {
        fs::write(self.dir.join("home/.bash_profile"), "sleep 1\n").unwrap();
    }

    fn serve_mode(&self, mode: &str) {
        fs::write(self.dir.join("serve-mode"), mode).unwrap();
    }

    fn emitted(&self, name: &str) -> Vec<(Instant, Value)> {
        self.emitted
            .lock()
            .unwrap()
            .iter()
            .filter(|(_, n, _)| *n == name)
            .map(|(at, _, v)| (*at, v.clone()))
            .collect()
    }

    /// Waits until `count` events named `name` were emitted.
    async fn wait_for(&self, name: &str, count: usize) -> Vec<(Instant, Value)> {
        let start = Instant::now();
        loop {
            let got = self.emitted(name);
            if got.len() >= count {
                return got;
            }
            assert!(
                start.elapsed() < Duration::from_secs(10),
                "waited for {count} {name}, got {got:?}"
            );
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    }

    fn grandchild(&self) -> u32 {
        fs::read_to_string(self.dir.join("grandchild"))
            .unwrap()
            .trim()
            .parse()
            .unwrap()
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// Whether `pid` still runs. A zombie no parent has reaped yet counts as gone; on Linux, /proc says
/// which it is.
fn alive(pid: u32) -> bool {
    let Ok(pid) = libc::pid_t::try_from(pid) else {
        return false;
    };
    // SAFETY: signal 0 sends nothing; it only checks that the process exists.
    let exists = unsafe { libc::kill(pid, 0) } == 0;
    exists
        && fs::read_to_string(format!("/proc/{pid}/stat")).map_or(true, |stat| {
            !stat
                .rsplit(')')
                .next()
                .is_some_and(|s| s.trim_start().starts_with('Z'))
        })
}

async fn gone(pid: u32) -> bool {
    for _ in 0..150 {
        if !alive(pid) {
            return true;
        }
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    false
}

fn args(a: &[&str]) -> Vec<String> {
    a.iter().map(|s| (*s).to_owned()).collect()
}

#[tokio::test(flavor = "multi_thread")]
async fn a_one_shot_streams_its_lines_and_reports_its_end() {
    let f = Fixture::new("lines");
    let end = f
        .sup
        .run("r1".into(), args(&["lines"]), None)
        .await
        .unwrap();

    assert_eq!(end.exit_code, Some(3));
    assert_eq!(end.events.len(), 2);
    assert_eq!(end.events[0]["type"], "version");
    assert_eq!(end.events[1]["type"], "result");
    assert_eq!(end.logs[0]["message"], "careful");
    assert_eq!(end.invalid.len(), 1);
    assert_eq!(end.invalid[0].line, "not json");
    assert!(!end.cancelled && !end.timed_out);

    let lines: Vec<Value> = f.emitted(RUN_LINE).into_iter().map(|(_, v)| v).collect();
    assert_eq!(lines.len(), 4);
    assert!(lines.iter().all(|l| l["run_id"] == "r1"));
    let stdout: Vec<&Value> = lines.iter().filter(|l| l["kind"] != "log").collect();
    assert_eq!(stdout[0]["kind"], "event");
    assert_eq!(
        stdout[1],
        &json!({"run_id": "r1", "kind": "invalid", "stream": "stdout", "line": "not json"})
    );
    assert_eq!(stdout[2]["event"]["type"], "result");
    assert!(lines
        .iter()
        .any(|l| l["kind"] == "log" && l["event"]["level"] == "warn"));

    let ends = f.emitted(RUN_END);
    assert_eq!(ends.len(), 1);
    assert_eq!(ends[0].1["exit_code"], 3);
    assert_eq!(ends[0].1["run_id"], "r1");
}

#[tokio::test(flavor = "multi_thread")]
async fn a_deadline_kills_the_whole_process_group() {
    let f = Fixture::new("deadline");
    let end = f
        .sup
        .run(
            "r1".into(),
            args(&["hang"]),
            Some(Duration::from_millis(500)),
        )
        .await
        .unwrap();

    assert!(end.timed_out);
    assert!(!end.cancelled);
    assert_eq!(end.exit_code, None, "a signal ended the stub");
    assert_eq!(end.events[0]["type"], "progress");
    assert!(
        gone(f.grandchild()).await,
        "the grandchild sleep still runs"
    );
}

#[tokio::test(flavor = "multi_thread")]
async fn cancel_kills_the_whole_process_group() {
    let f = Fixture::new("cancel");
    let sup = f.sup.clone();
    let run = tokio::spawn(async move { sup.run("r1".into(), args(&["hang"]), None).await });
    f.wait_for(RUN_LINE, 1).await;

    assert!(!f.sup.cancel("r2"), "no such run");
    assert!(f.sup.cancel("r1"));
    let end = run.await.unwrap().unwrap();
    assert!(end.cancelled);
    assert!(!end.timed_out);
    assert!(
        gone(f.grandchild()).await,
        "the grandchild sleep still runs"
    );
    assert!(!f.sup.cancel("r1"), "the run is over");
}

#[tokio::test(flavor = "multi_thread")]
async fn a_cancel_while_the_login_shell_is_read_stops_the_run_before_it_starts() {
    let f = Fixture::new("early-cancel");
    f.slow_profile();
    let sup = f.sup.clone();
    let run = tokio::spawn(async move { sup.run("r1".into(), args(&["hang"]), None).await });
    tokio::time::sleep(Duration::from_millis(200)).await;

    assert!(f.sup.cancel("r1"));
    let end = run.await.unwrap().unwrap();
    assert!(end.cancelled);
    assert_eq!(end.exit_code, None);
    assert!(end.events.is_empty());
    assert!(!f.dir.join("grandchild").exists(), "the CLI started");
    assert_eq!(f.emitted(RUN_END).len(), 1);
}

#[tokio::test(flavor = "multi_thread")]
async fn a_child_gets_the_login_shell_environment_and_json() {
    let f = Fixture::new("env");
    let resolution = f.sup.resolution().await;
    assert_eq!(resolution.path, Some(f.dir.join("bin/agentx")));
    assert_eq!(serde_json::to_value(resolution.found).unwrap(), "bundled");
    assert_eq!(resolution.app_version, "1.2.0");
    assert!(!resolution.debug);

    let end = f.sup.run("r1".into(), args(&["env"]), None).await.unwrap();
    assert_eq!(end.events[0]["profile"], "from-profile");
    assert_eq!(end.events[0]["args"], "env --json");
}

#[tokio::test(flavor = "multi_thread")]
async fn serve_takes_requests_logs_its_stderr_and_stops_with_the_window() {
    let f = Fixture::new("serve");
    f.serve_mode("echo");
    let child = f.sup.serve_start().await.unwrap();
    assert_eq!(f.sup.serve_start().await.unwrap(), child, "one serve child");
    let first = f.wait_for(SERVE_LINE, 1).await;
    assert_eq!(first[0].1["child_id"], child);
    assert_eq!(first[0].1["event"]["type"], "snapshot");

    let request = r#"{"type":"refresh","request_id":"q1"}"#;
    f.sup.serve_request(request).await.unwrap();
    let lines = f.wait_for(SERVE_LINE, 2).await;
    assert_eq!(
        lines[1].1["event"]["request"],
        json!({"type": "refresh", "request_id": "q1"})
    );
    assert!(f.sup.serve_request("not json").await.is_err());
    assert!(f.sup.serve_request("[1]").await.is_err());

    f.sup.shutdown().await;
    let exits = f.wait_for(SERVE_EXIT, 1).await;
    assert_eq!(exits[0].1["exit_code"], 0);
    assert_eq!(exits[0].1["restarting"], false);
    let lines = f.emitted(SERVE_LINE);
    assert_eq!(lines.last().unwrap().1["event"]["type"], "result");
    let log = fs::read_to_string(f.dir.join("logs/serve.log")).unwrap();
    assert!(log.contains("serve started"), "{log}");
    assert!(f.sup.serve_request(request).await.is_err(), "serve is gone");
}

#[tokio::test(flavor = "multi_thread")]
async fn serve_is_restarted_with_backoff_and_a_fresh_environment() {
    let f = Fixture::new("restart");
    f.serve_mode("crash");
    f.sup.serve_start().await.unwrap();
    let exits = f.wait_for(SERVE_EXIT, 1).await;
    f.profile("after-restart");
    let exits_and_lines = f.wait_for(SERVE_EXIT, 3).await;
    f.sup.shutdown().await;

    let lines = f.emitted(SERVE_LINE);
    let summary: Vec<(Value, Value, Value)> = exits_and_lines
        .iter()
        .map(|(_, e)| {
            (
                e["child_id"].clone(),
                e["exit_code"].clone(),
                e["restart_in_ms"].clone(),
            )
        })
        .collect();
    assert_eq!(
        summary[..3],
        [
            (json!(1), json!(1), json!(500)),
            (json!(2), json!(1), json!(1000)),
            (json!(3), json!(1), json!(2000)),
        ]
    );
    assert_eq!(
        exits[0].1["stderr"].as_array().unwrap().last().unwrap(),
        "boom"
    );
    // Each child starts only after its delay.
    let started = |id: u64| lines.iter().find(|(_, l)| l["child_id"] == id).unwrap().0;
    assert!(started(2) - exits_and_lines[0].0 >= Duration::from_millis(450));
    assert!(started(3) - exits_and_lines[1].0 >= Duration::from_millis(950));
    // The profile changed after the first child: the next ones see it.
    assert_eq!(lines[0].1["event"]["profile"], "from-profile");
    assert_eq!(
        lines.iter().find(|(_, l)| l["child_id"] == 3).unwrap().1["event"]["profile"],
        "after-restart"
    );
}

#[tokio::test(flavor = "multi_thread")]
async fn a_refused_serve_is_not_restarted_in_a_loop() {
    let f = Fixture::new("refused");
    f.serve_mode("locked");
    let first = f.sup.serve_start().await.unwrap();
    let exits = f.wait_for(SERVE_EXIT, 1).await;
    assert_eq!(exits[0].1["exit_code"], 6);
    assert_eq!(exits[0].1["restarting"], false);
    assert_eq!(f.emitted(SERVE_LINE)[1].1["event"]["type"], "error");

    tokio::time::sleep(Duration::from_millis(1200)).await;
    assert_eq!(f.emitted(SERVE_EXIT).len(), 1, "serve was restarted");
    // Retry starts it again.
    let second = f.sup.serve_start().await.unwrap();
    assert_ne!(second, first);
    f.wait_for(SERVE_EXIT, 2).await;
}
