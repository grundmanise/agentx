// The user's login shell environment and the CLI it finds. A GUI app starts with a minimal PATH
// and none of the SSH agent or credential helper variables a terminal has, so a fetch that works
// in the terminal would fail from the app. Every CLI child gets the environment the user's login
// shell sets up instead.

use std::collections::HashMap;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::time::Duration;

use serde::Serialize;
use tokio::process::Command;

/// How long the login shell may take to print its environment.
pub const CAPTURE_TIMEOUT: Duration = Duration::from_secs(5);

/// Printed before the environment, so that what an interactive shell prints on start is skipped.
const MARKER: &str = "__agentx_env__";

/// Runs `shell` as an interactive login shell with `base` as its environment and returns the
/// environment it sets up. The same command works in bash, zsh and fish. The shell runs in a
/// process group of its own, which is killed when it does not finish within `timeout`.
pub async fn capture(
    shell: &str,
    base: &HashMap<String, String>,
    timeout: Duration,
) -> Result<HashMap<String, String>, String> {
    let child = Command::new(shell)
        .args([
            "-l",
            "-i",
            "-c",
            &format!("printf '%s' {MARKER}; command env -0"),
        ])
        .env_clear()
        .envs(base)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .process_group(0)
        .kill_on_drop(true)
        .spawn()
        .map_err(|err| format!("{shell}: {err}"))?;
    let pid = child.id();
    match tokio::time::timeout(timeout, child.wait_with_output()).await {
        Ok(Ok(out)) => parse(&out.stdout).ok_or_else(|| format!("{shell} printed no environment")),
        Ok(Err(err)) => Err(format!("{shell}: {err}")),
        Err(_) => {
            if let Some(pid) = pid {
                crate::supervisor::signal_group(pid, libc::SIGKILL);
            }
            Err(format!(
                "{shell} did not print its environment within {}s",
                timeout.as_secs()
            ))
        }
    }
}

/// The variables after the marker, as `env -0` prints them.
fn parse(out: &[u8]) -> Option<HashMap<String, String>> {
    let marker = MARKER.as_bytes();
    let start = out.windows(marker.len()).position(|w| w == marker)? + marker.len();
    let env: HashMap<String, String> = out[start..]
        .split(|&b| b == 0)
        .filter_map(|entry| {
            let entry = String::from_utf8_lossy(entry);
            let (key, value) = entry.split_once('=')?;
            (!key.is_empty()).then(|| (key.to_owned(), value.to_owned()))
        })
        .collect();
    (!env.is_empty()).then_some(env)
}

/// Where the CLI was found.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Found {
    /// Next to the app's executable, where a release build puts it.
    Bundled,
    /// On the login shell's `PATH`.
    Path,
}

/// Finds `agentx` next to the app's executable, then on `path`.
pub fn resolve(exe_dir: Option<&Path>, path: Option<&str>) -> Option<(PathBuf, Found)> {
    if let Some(dir) = exe_dir {
        let bundled = dir.join("agentx");
        if is_executable(&bundled) {
            return Some((bundled, Found::Bundled));
        }
    }
    path?
        .split(':')
        .filter(|dir| !dir.is_empty())
        .map(|dir| Path::new(dir).join("agentx"))
        .find(|candidate| is_executable(candidate))
        .map(|found| (found, Found::Path))
}

fn is_executable(path: &Path) -> bool {
    path.metadata()
        .is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_skips_what_the_shell_printed_first() {
        let out = b"Welcome!\n__agentx_env__PATH=/usr/bin:/bin\0EMPTY=\0A=b=c\0\0";
        let env = parse(out).unwrap();
        assert_eq!(env["PATH"], "/usr/bin:/bin");
        assert_eq!(env["EMPTY"], "");
        assert_eq!(env["A"], "b=c");
        assert_eq!(env.len(), 3);
    }

    #[test]
    fn parse_needs_the_marker() {
        assert!(parse(b"PATH=/bin\0").is_none());
        assert!(parse(b"__agentx_env__").is_none());
    }
}
