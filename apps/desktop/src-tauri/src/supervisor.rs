// The CLI's process supervisor: one-shot commands, and the one serve child the window keeps open.
// It knows nothing about Tauri; what it reports goes through an `Emit` function, which the app
// turns into Tauri events and the tests collect.
//
// Every child gets the login shell's environment, runs in a process group of its own and is
// stopped by signalling that group, so the git and the helpers it started stop with it.

use std::collections::{HashMap, VecDeque};
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::Serialize;
use serde_json::Value;
use tokio::io::{AsyncBufReadExt, AsyncRead, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin, Command};
use tokio::sync::{mpsc, watch, Notify};

use crate::logfile::RotatingLog;
use crate::shell::{self, Found};

/// Reports to the frontend: an event name and its payload.
pub type Emit = Arc<dyn Fn(&'static str, Value) + Send + Sync>;

/// A line of a one-shot run, with its `run_id`.
pub const RUN_LINE: &str = "cli-run";
/// The end of a one-shot run.
pub const RUN_END: &str = "cli-run-end";
/// A line of the serve child's stdout, with its `child_id`.
pub const SERVE_LINE: &str = "serve-event";
/// The serve child ended.
pub const SERVE_EXIT: &str = "serve-exit";

/// How long a stopped child has between SIGTERM and SIGKILL.
const KILL_GRACE: Duration = Duration::from_secs(2);
/// How long the output a child left in its pipes is read after it exits. A grandchild that keeps
/// a pipe open must not hold the run open.
const DRAIN: Duration = Duration::from_secs(1);
/// How long closing the window waits for serve to end after its stdin closes.
const SERVE_STOP: Duration = Duration::from_secs(3);
/// The serve restart backoff: the first delay, the cap, and how long a child must have run for
/// the next delay to start from the first again.
const BACKOFF_FIRST: Duration = Duration::from_millis(500);
const BACKOFF_CAP: Duration = Duration::from_secs(30);
const BACKOFF_RESET: Duration = Duration::from_secs(60);
/// Exit codes after which serve is not restarted: git missing (2), serve refused (6: another
/// serve holds `serve.lock`, or the watch limit), and the account repo unusable (8). A restart
/// cannot fix them; the startup screen shows them and offers Retry, which starts serve again.
const NO_RESTART: [i32; 3] = [2, 6, 8];
/// How many cancels of runs that have not arrived yet are kept.
const EARLY_CANCELS: usize = 64;
/// How many of serve's last stderr lines its exit carries.
const STDERR_TAIL: usize = 20;

/// The serve log: 5 files of 5 MB.
const LOG_NAME: &str = "serve.log";
const LOG_MAX_BYTES: u64 = 5 * 1024 * 1024;
const LOG_FILES: usize = 5;

pub struct Config {
    /// The directory of the app's executable, where a release build puts the CLI.
    pub exe_dir: Option<PathBuf>,
    /// The app's own environment. The login shell starts from it, and children get it when the
    /// shell cannot be read.
    pub base_env: HashMap<String, String>,
    /// Where serve's stderr is written.
    pub log_dir: PathBuf,
    pub app_version: String,
    pub debug: bool,
}

/// What the frontend learns about the CLI before running it.
#[derive(Clone, Debug, Serialize)]
pub struct Resolution {
    /// The CLI's path; absent when none was found.
    pub path: Option<PathBuf>,
    pub found: Option<Found>,
    pub app_version: String,
    /// Whether this is a debug build of the app, which runs any CLI version.
    pub debug: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Stream {
    Stdout,
    Stderr,
}

/// A line a child wrote that is not a JSON object. It is reported, never dropped.
#[derive(Clone, Debug, Serialize)]
pub struct Invalid {
    pub stream: Stream,
    pub line: String,
}

/// One line of a child's output.
#[derive(Clone, Debug, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Line {
    /// An event on stdout.
    Event {
        event: Value,
    },
    /// A log event on stderr.
    Log {
        event: Value,
    },
    Invalid(Invalid),
}

/// How a one-shot run ended.
#[derive(Clone, Debug, Serialize)]
pub struct RunEnd {
    pub run_id: String,
    /// The exit code; absent when a signal ended the CLI.
    pub exit_code: Option<i32>,
    pub events: Vec<Value>,
    pub logs: Vec<Value>,
    pub invalid: Vec<Invalid>,
    pub cancelled: bool,
    pub timed_out: bool,
}

/// How a serve child ended.
#[derive(Clone, Debug, Serialize)]
pub struct ServeExit {
    pub child_id: u64,
    /// The exit code; absent when a signal ended it or it could not start.
    pub exit_code: Option<i32>,
    /// Its last stderr lines.
    pub stderr: Vec<String>,
    /// Whether the supervisor starts a new child, after `restart_in_ms`.
    pub restarting: bool,
    pub restart_in_ms: Option<u64>,
}

/// The login shell's environment and the CLI found on its `PATH`.
struct Shell {
    env: HashMap<String, String>,
    cli: Option<(PathBuf, Found)>,
}

/// A one-shot run in progress. It is registered before the CLI starts, so that a cancel that
/// arrives while the login shell is still being read stops the run before it starts.
#[derive(Default)]
struct RunControl {
    /// The CLI's pid, which leads its process group; 0 until it starts.
    pid: AtomicU32,
    cancelled: AtomicBool,
    stop: Notify,
}

/// The one-shot runs going, and cancels that arrived before their run. Tauri may handle a cancel
/// before the run it names, when both are sent at once: such a cancel is kept and stops the run as
/// it arrives. Both are under one lock, so that a cancel and its run cannot miss each other.
#[derive(Default)]
struct Runs {
    going: HashMap<String, Arc<RunControl>>,
    early: VecDeque<String>,
}

#[derive(Default)]
struct ServeState {
    child_id: u64,
    pid: Option<u32>,
    stdin: Option<ChildStdin>,
    running: bool,
    stopping: bool,
}

pub struct Supervisor {
    config: Config,
    emit: Emit,
    shell: tokio::sync::Mutex<Option<Arc<Shell>>>,
    runs: Mutex<Runs>,
    serve: tokio::sync::Mutex<ServeState>,
    serve_running: watch::Sender<bool>,
    serve_stop: Notify,
    next_child: AtomicU64,
    log: Mutex<RotatingLog>,
}

impl Supervisor {
    pub fn new(config: Config, emit: Emit) -> Arc<Self> {
        let log = RotatingLog::new(config.log_dir.clone(), LOG_NAME, LOG_MAX_BYTES, LOG_FILES);
        Arc::new(Supervisor {
            config,
            emit,
            shell: tokio::sync::Mutex::new(None),
            runs: Mutex::new(Runs::default()),
            serve: tokio::sync::Mutex::new(ServeState::default()),
            serve_running: watch::Sender::new(false),
            serve_stop: Notify::new(),
            next_child: AtomicU64::new(1),
            log: Mutex::new(log),
        })
    }

    /// The login shell's environment, captured on first use.
    async fn shell(&self) -> Arc<Shell> {
        let mut shell = self.shell.lock().await;
        if let Some(shell) = &*shell {
            return shell.clone();
        }
        let captured = Arc::new(self.capture().await);
        *shell = Some(captured.clone());
        captured
    }

    /// Captures the login shell's environment again.
    async fn refresh_shell(&self) -> Arc<Shell> {
        let mut shell = self.shell.lock().await;
        let captured = Arc::new(self.capture().await);
        *shell = Some(captured.clone());
        captured
    }

    async fn capture(&self) -> Shell {
        let base = &self.config.base_env;
        let env = match base.get("SHELL").filter(|s| !s.is_empty()) {
            None => {
                eprintln!("warning: SHELL is not set; the CLI gets the app's own environment");
                base.clone()
            }
            Some(sh) => match shell::capture(sh, base, shell::CAPTURE_TIMEOUT).await {
                Ok(env) => env,
                Err(err) => {
                    eprintln!("warning: {err}; the CLI gets the app's own environment");
                    base.clone()
                }
            },
        };
        let cli = shell::resolve(
            self.config.exe_dir.as_deref(),
            env.get("PATH").map(String::as_str),
        );
        Shell { env, cli }
    }

    /// Where the CLI is, with the app's version and build.
    pub async fn resolution(&self) -> Resolution {
        let shell = self.shell().await;
        Resolution {
            path: shell.cli.as_ref().map(|(path, _)| path.clone()),
            found: shell.cli.as_ref().map(|(_, found)| *found),
            app_version: self.config.app_version.clone(),
            debug: self.config.debug,
        }
    }

    fn command(&self, shell: &Shell, args: &[String]) -> Result<Command, String> {
        let (cli, _) = shell
            .cli
            .as_ref()
            .ok_or("agentx was not found next to the app or on PATH")?;
        let mut cmd = Command::new(cli);
        cmd.args(json_args(args))
            .env_clear()
            .envs(&shell.env)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .process_group(0);
        Ok(cmd)
    }

    /// Runs `agentx <args> --json`, emits each line as it arrives and the end, and returns the
    /// end. A deadline stops the run when it passes.
    pub async fn run(
        &self,
        run_id: String,
        args: Vec<String>,
        deadline: Option<Duration>,
    ) -> Result<RunEnd, String> {
        let control = Arc::new(RunControl::default());
        {
            let mut runs = self.runs.lock().unwrap();
            if runs.going.contains_key(&run_id) {
                return Err(format!("run {run_id} is already running"));
            }
            if let Some(at) = runs.early.iter().position(|id| *id == run_id) {
                runs.early.remove(at);
                control.cancelled.store(true, Ordering::SeqCst);
            }
            runs.going.insert(run_id.clone(), control.clone());
        }
        let end = self.start_run(&run_id, &args, &control, deadline).await;
        self.runs.lock().unwrap().going.remove(&run_id);
        let end = end?;
        (self.emit)(RUN_END, to_value(&end));
        Ok(end)
    }

    async fn start_run(
        &self,
        run_id: &str,
        args: &[String],
        control: &RunControl,
        deadline: Option<Duration>,
    ) -> Result<RunEnd, String> {
        let shell = self.shell().await;
        let mut end = RunEnd {
            run_id: run_id.to_owned(),
            exit_code: None,
            events: vec![],
            logs: vec![],
            invalid: vec![],
            cancelled: false,
            timed_out: false,
        };
        if control.cancelled.load(Ordering::SeqCst) {
            end.cancelled = true;
            return Ok(end);
        }
        let mut child = self
            .command(&shell, args)?
            .stdin(Stdio::null())
            .spawn()
            .map_err(|err| format!("agentx: {err}"))?;
        let pid = child.id().ok_or("agentx exited at once")?;
        control.pid.store(pid, Ordering::SeqCst);
        self.watch_run(&mut end, &mut child, pid, control, deadline)
            .await;
        Ok(end)
    }

    /// Emits the run's lines as they arrive into `end`, until the CLI exits.
    async fn watch_run(
        &self,
        end: &mut RunEnd,
        child: &mut Child,
        pid: u32,
        control: &RunControl,
        deadline: Option<Duration>,
    ) {
        let run_id = end.run_id.clone();
        let mut lines = read_lines(child);
        let mut record = |stream, text| {
            let Some(line) = classify(stream, text) else {
                return;
            };
            (self.emit)(RUN_LINE, with_id("run_id", &run_id, &line));
            match line {
                Line::Event { event } => end.events.push(event),
                Line::Log { event } => end.logs.push(event),
                Line::Invalid(invalid) => end.invalid.push(invalid),
            }
        };
        let deadline = async {
            match deadline {
                Some(d) => tokio::time::sleep(d).await,
                None => std::future::pending().await,
            }
        };
        tokio::pin!(deadline);
        let mut timed_out = false;
        let mut open = true;
        let status = loop {
            tokio::select! {
                line = lines.recv(), if open => match line {
                    Some((stream, text)) => record(stream, text),
                    None => open = false,
                },
                status = child.wait() => break status,
                () = &mut deadline, if !timed_out => {
                    timed_out = true;
                    stop(pid);
                }
                // A cancel that arrived while the CLI was starting left its notification here.
                () = control.stop.notified() => stop(pid),
            }
        };
        drain(&mut lines, &mut record).await;
        end.exit_code = status.ok().and_then(|s| s.code());
        end.cancelled = control.cancelled.load(Ordering::SeqCst);
        end.timed_out = timed_out;
    }

    /// Stops a one-shot run: its process group gets SIGTERM, on which agentx reports the stop and
    /// exits 9, then SIGKILL if it is still there. Returns false when no such run is going; the
    /// cancel is then kept, and stops the run if it arrives later.
    pub fn cancel(&self, run_id: &str) -> bool {
        let mut runs = self.runs.lock().unwrap();
        let Some(control) = runs.going.get(run_id).cloned() else {
            if runs.early.len() == EARLY_CANCELS {
                runs.early.pop_front();
            }
            runs.early.push_back(run_id.to_owned());
            return false;
        };
        drop(runs);
        control.cancelled.store(true, Ordering::SeqCst);
        control.stop.notify_one();
        true
    }

    /// Starts the serve child unless one is running, and returns its child id.
    pub async fn serve_start(self: &Arc<Self>) -> Result<u64, String> {
        let mut state = self.serve.lock().await;
        if state.running {
            return Ok(state.child_id);
        }
        let shell = self.shell().await;
        let child = self.spawn_serve(&shell, &mut state)?;
        state.running = true;
        state.stopping = false;
        self.serve_running.send_replace(true);
        tokio::spawn(self.clone().keep_serving(child));
        Ok(state.child_id)
    }

    fn spawn_serve(&self, shell: &Shell, state: &mut ServeState) -> Result<Child, String> {
        let mut child = self
            .command(shell, &["serve".to_owned()])?
            .stdin(Stdio::piped())
            .spawn()
            .map_err(|err| format!("agentx: {err}"))?;
        state.child_id = self.next_child.fetch_add(1, Ordering::SeqCst);
        state.pid = child.id();
        state.stdin = child.stdin.take();
        Ok(child)
    }

    /// Watches the serve child and restarts it with backoff until the window closes or it ends in
    /// a way a restart cannot fix.
    async fn keep_serving(self: Arc<Self>, mut child: Child) {
        let mut delay = BACKOFF_FIRST;
        loop {
            let child_id = self.serve.lock().await.child_id;
            let started = Instant::now();
            let (exit_code, stderr) = self.watch_serve(child_id, &mut child).await;
            if started.elapsed() >= BACKOFF_RESET {
                delay = BACKOFF_FIRST;
            }
            {
                let mut state = self.serve.lock().await;
                state.pid = None;
                state.stdin = None;
                let restart = !state.stopping && exit_code.is_none_or(|c| !NO_RESTART.contains(&c));
                self.serve_exited(child_id, exit_code, stderr, restart.then_some(delay));
                if !restart {
                    self.serve_ended(&mut state);
                    return;
                }
            }
            match self.restart(&mut delay).await {
                Some(next) => child = next,
                None => return,
            }
        }
    }

    /// Waits `delay`, then starts a new serve child, doubling the delay up to the cap each time.
    /// A child that cannot start is reported as one that exited, and tried again. Returns None
    /// once the window is closing.
    async fn restart(&self, delay: &mut Duration) -> Option<Child> {
        loop {
            let wait = *delay;
            *delay = (*delay * 2).min(BACKOFF_CAP);
            tokio::select! {
                () = tokio::time::sleep(wait) => {}
                () = self.serve_stop.notified() => {}
            }
            // Closing the window wakes the wait: stop before reading the login shell for nothing.
            {
                let mut state = self.serve.lock().await;
                if state.stopping {
                    self.serve_ended(&mut state);
                    return None;
                }
            }
            // The environment is captured again, so that a change to the user's shell profile
            // reaches the next child.
            let shell = self.refresh_shell().await;
            let mut state = self.serve.lock().await;
            if state.stopping {
                self.serve_ended(&mut state);
                return None;
            }
            match self.spawn_serve(&shell, &mut state) {
                Ok(child) => return Some(child),
                Err(err) => {
                    state.child_id = self.next_child.fetch_add(1, Ordering::SeqCst);
                    self.serve_exited(state.child_id, None, vec![err], Some(*delay));
                }
            }
        }
    }

    fn serve_exited(
        &self,
        child_id: u64,
        exit_code: Option<i32>,
        stderr: Vec<String>,
        restart_in: Option<Duration>,
    ) {
        let exit = ServeExit {
            child_id,
            exit_code,
            stderr,
            restarting: restart_in.is_some(),
            restart_in_ms: restart_in.map(|d| d.as_millis() as u64),
        };
        (self.emit)(SERVE_EXIT, to_value(&exit));
    }

    fn serve_ended(&self, state: &mut ServeState) {
        state.running = false;
        state.stopping = false;
        self.serve_running.send_replace(false);
    }

    /// Emits the serve child's stdout, writes its stderr to the log, and returns its exit code and
    /// last stderr lines once it exits.
    async fn watch_serve(&self, child_id: u64, child: &mut Child) -> (Option<i32>, Vec<String>) {
        let mut tail = VecDeque::with_capacity(STDERR_TAIL);
        let mut lines = read_lines(child);
        let mut record = |stream, text: String| {
            if stream == Stream::Stderr {
                self.log.lock().unwrap().write_line(&text);
                if tail.len() == STDERR_TAIL {
                    tail.pop_front();
                }
                tail.push_back(text);
                return;
            }
            if let Some(line) = classify(stream, text) {
                (self.emit)(SERVE_LINE, with_id("child_id", child_id, &line));
            }
        };
        let mut open = true;
        let status = loop {
            tokio::select! {
                line = lines.recv(), if open => match line {
                    Some((stream, text)) => record(stream, text),
                    None => open = false,
                },
                status = child.wait() => break status,
            }
        };
        drain(&mut lines, &mut record).await;
        (status.ok().and_then(|s| s.code()), tail.into())
    }

    /// Writes one request to the serve child's stdin, as one line.
    pub async fn serve_request(&self, line: &str) -> Result<(), String> {
        let request: Value =
            serde_json::from_str(line).map_err(|err| format!("not a JSON request: {err}"))?;
        if !request.is_object() {
            return Err("a serve request is a JSON object".to_owned());
        }
        let mut state = self.serve.lock().await;
        let stdin = state.stdin.as_mut().ok_or("serve is not running")?;
        let mut bytes = request.to_string().into_bytes();
        bytes.push(b'\n');
        stdin
            .write_all(&bytes)
            .await
            .map_err(|err| format!("serve's stdin: {err}"))?;
        stdin
            .flush()
            .await
            .map_err(|err| format!("serve's stdin: {err}"))
    }

    /// Stops everything when the window closes: one-shot runs are killed; serve's stdin is closed,
    /// on which it emits its `result` and exits, and its process group is killed if it has not
    /// within 3s.
    pub async fn shutdown(&self) {
        for control in self.runs.lock().unwrap().going.values() {
            control.cancelled.store(true, Ordering::SeqCst);
            match control.pid.load(Ordering::SeqCst) {
                0 => {}
                pid => signal_group(pid, libc::SIGKILL),
            }
        }
        let pid = {
            let mut state = self.serve.lock().await;
            if !state.running {
                return;
            }
            state.stopping = true;
            state.stdin = None;
            state.pid
        };
        self.serve_stop.notify_one();
        let mut running = self.serve_running.subscribe();
        let ended = tokio::time::timeout(SERVE_STOP, running.wait_for(|r| !r)).await;
        if ended.is_err() {
            if let Some(pid) = pid {
                signal_group(pid, libc::SIGKILL);
            }
        }
    }
}

/// The arguments with `--json` added, before a `--` that ends the flags.
fn json_args(args: &[String]) -> Vec<String> {
    let mut out = args.to_vec();
    if !out.iter().any(|a| a == "--json") {
        let at = out.iter().position(|a| a == "--").unwrap_or(out.len());
        out.insert(at, "--json".to_owned());
    }
    out
}

/// Reads the child's stdout and stderr, each line tagged with its stream, in the order they
/// arrive.
fn read_lines(child: &mut Child) -> mpsc::UnboundedReceiver<(Stream, String)> {
    let (tx, rx) = mpsc::unbounded_channel();
    if let Some(out) = child.stdout.take() {
        tokio::spawn(forward(out, Stream::Stdout, tx.clone()));
    }
    if let Some(err) = child.stderr.take() {
        tokio::spawn(forward(err, Stream::Stderr, tx));
    }
    rx
}

async fn forward(
    pipe: impl AsyncRead + Unpin,
    stream: Stream,
    tx: mpsc::UnboundedSender<(Stream, String)>,
) {
    let mut reader = BufReader::new(pipe);
    let mut buf = Vec::new();
    loop {
        buf.clear();
        match reader.read_until(b'\n', &mut buf).await {
            Ok(0) | Err(_) => return,
            Ok(_) => {
                let text = String::from_utf8_lossy(&buf);
                let text = text.trim_end_matches(['\n', '\r']).to_owned();
                if tx.send((stream, text)).is_err() {
                    return;
                }
            }
        }
    }
}

/// Reads what the child left in its pipes after it exited.
async fn drain(
    lines: &mut mpsc::UnboundedReceiver<(Stream, String)>,
    record: &mut impl FnMut(Stream, String),
) {
    let _ = tokio::time::timeout(DRAIN, async {
        while let Some((stream, text)) = lines.recv().await {
            record(stream, text);
        }
    })
    .await;
}

/// A line as an event, a log event or an invalid line. A blank line carries nothing.
fn classify(stream: Stream, text: String) -> Option<Line> {
    if text.trim().is_empty() {
        return None;
    }
    Some(match serde_json::from_str::<Value>(&text) {
        Ok(event) if event.is_object() => match stream {
            Stream::Stdout => Line::Event { event },
            Stream::Stderr => Line::Log { event },
        },
        _ => Line::Invalid(Invalid { stream, line: text }),
    })
}

fn to_value(v: &impl Serialize) -> Value {
    serde_json::to_value(v).unwrap_or(Value::Null)
}

/// The line with `key` set to `id`, as the frontend receives it.
fn with_id(key: &str, id: impl Serialize, line: &Line) -> Value {
    let mut value = to_value(line);
    if let Value::Object(map) = &mut value {
        map.insert(key.to_owned(), to_value(&id));
    }
    value
}

/// Stops a process group: SIGTERM now, SIGKILL after the grace period.
fn stop(pid: u32) {
    signal_group(pid, libc::SIGTERM);
    tokio::spawn(async move {
        tokio::time::sleep(KILL_GRACE).await;
        signal_group(pid, libc::SIGKILL);
    });
}

/// Sends `signal` to the process group `pid` leads. A group that is gone is not an error.
pub(crate) fn signal_group(pid: u32, signal: i32) {
    if let Ok(pid) = libc::pid_t::try_from(pid) {
        // SAFETY: killpg only sends a signal; the group was started by this process.
        unsafe {
            libc::killpg(pid, signal);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn json_goes_before_the_end_of_the_flags() {
        let args = |a: &[&str]| a.iter().map(|s| (*s).to_owned()).collect::<Vec<_>>();
        assert_eq!(json_args(&args(&["version"])), args(&["version", "--json"]));
        assert_eq!(
            json_args(&args(&["skill", "add", "--", "-x"])),
            args(&["skill", "add", "--json", "--", "-x"])
        );
        assert_eq!(
            json_args(&args(&["--json", "doctor"])),
            args(&["--json", "doctor"])
        );
    }

    #[test]
    fn classify_reports_what_is_not_a_json_object() {
        assert!(classify(Stream::Stdout, " ".to_owned()).is_none());
        assert!(matches!(
            classify(Stream::Stdout, r#"{"type":"result"}"#.to_owned()),
            Some(Line::Event { .. })
        ));
        assert!(matches!(
            classify(Stream::Stderr, r#"{"type":"log"}"#.to_owned()),
            Some(Line::Log { .. })
        ));
        assert!(matches!(
            classify(Stream::Stdout, "[1]".to_owned()),
            Some(Line::Invalid(Invalid {
                stream: Stream::Stdout,
                ..
            }))
        ));
    }
}
