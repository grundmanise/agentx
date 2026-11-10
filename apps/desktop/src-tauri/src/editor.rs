// The editor a skill opens in (ADR 0013). The app finds the editors it knows on this machine, and
// opens a skill's file in one of them or with a command line the user typed. The system's default
// application is the opener plugin's, which the frontend calls itself. Nothing here runs a shell:
// a command line is split into a program and its arguments, the file's path is appended, and the
// program runs with the login shell's environment, as the CLI does, so a program the user's
// terminal finds is found here too.

use std::collections::HashMap;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tokio::process::Command;

/// The editor a skill opens in, as the app's preferences keep it.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Editor {
    /// The system's application for the file's type, through the opener plugin.
    #[default]
    System,
    /// An editor the app found on this machine, by its id; `name` is kept for when it is gone.
    App { id: String, name: String },
    /// A command line the user typed; the path is added as its last argument.
    Custom { command: String },
}

/// An editor the app knows, and where each platform keeps it.
struct Known {
    id: &'static str,
    name: &'static str,
    /// The application bundle's name on macOS, in `/Applications` or `~/Applications`.
    bundle: &'static str,
    /// The programs that start it on Linux, on `PATH`, in order of preference.
    programs: &'static [&'static str],
}

/// The editors the app looks for, in the order Settings lists them. Terminal editors are left out.
const KNOWN: &[Known] = &[
    Known {
        id: "vscode",
        name: "VS Code",
        bundle: "Visual Studio Code.app",
        programs: &["code"],
    },
    Known {
        id: "cursor",
        name: "Cursor",
        bundle: "Cursor.app",
        programs: &["cursor"],
    },
    Known {
        id: "zed",
        name: "Zed",
        bundle: "Zed.app",
        programs: &["zed", "zeditor"],
    },
    Known {
        id: "sublime",
        name: "Sublime Text",
        bundle: "Sublime Text.app",
        programs: &["subl"],
    },
];

/// The platforms detection knows.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Platform {
    MacOs,
    Linux,
}

impl Platform {
    pub fn current() -> Self {
        if cfg!(target_os = "macos") {
            Platform::MacOs
        } else {
            Platform::Linux
        }
    }
}

/// An editor found on this machine.
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct Detected {
    pub id: String,
    pub name: String,
    /// What starts it: the application bundle on macOS, the program on Linux.
    #[serde(skip)]
    pub launcher: PathBuf,
}

/// The editors of `KNOWN` present on this machine, in its order: on macOS each whose bundle is in
/// `/Applications` or `<home>/Applications`, on Linux each whose program is on `path`.
/// `is_launcher` says whether a candidate is there: a directory for a bundle, an executable file
/// for a program.
pub fn detect(
    platform: Platform,
    home: &Path,
    path: Option<&str>,
    is_launcher: impl Fn(&Path) -> bool,
) -> Vec<Detected> {
    KNOWN
        .iter()
        .filter_map(|known| {
            let launcher = match platform {
                Platform::MacOs => [PathBuf::from("/Applications"), home.join("Applications")]
                    .into_iter()
                    .map(|dir| dir.join(known.bundle))
                    .find(|candidate| is_launcher(candidate)),
                Platform::Linux => known.programs.iter().find_map(|program| {
                    path?
                        .split(':')
                        .filter(|dir| !dir.is_empty())
                        .map(|dir| Path::new(dir).join(program))
                        .find(|candidate| is_launcher(candidate))
                }),
            }?;
            Some(Detected {
                id: known.id.to_owned(),
                name: known.name.to_owned(),
                launcher,
            })
        })
        .collect()
}

/// Whether `path` is what starts an editor on this platform.
pub fn is_launcher(platform: Platform, path: &Path) -> bool {
    match platform {
        Platform::MacOs => path.is_dir(),
        Platform::Linux => path
            .metadata()
            .is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0),
    }
}

/// Why a command line could not be split.
#[derive(Debug, PartialEq, Eq)]
pub enum SplitError {
    Empty,
    UnclosedQuote,
}

/// Splits a command line into words the way a POSIX shell splits words, and nothing more: blanks
/// separate words; single quotes keep everything up to the next one; double quotes keep everything
/// up to the next unescaped one, where a backslash escapes `"`, `\`, `$` and `` ` ``; outside
/// quotes a backslash keeps the next character. No variable, `~`, glob or command is expanded.
pub fn split_command_line(line: &str) -> Result<Vec<String>, SplitError> {
    let mut words = Vec::new();
    let mut word = String::new();
    // A word starts at its first character or quote, so `''` is an empty word.
    let mut in_word = false;
    let mut chars = line.chars();
    while let Some(c) = chars.next() {
        match c {
            ' ' | '\t' | '\n' => {
                if in_word {
                    words.push(std::mem::take(&mut word));
                    in_word = false;
                }
            }
            '\'' => {
                in_word = true;
                loop {
                    match chars.next() {
                        Some('\'') => break,
                        Some(c) => word.push(c),
                        None => return Err(SplitError::UnclosedQuote),
                    }
                }
            }
            '"' => {
                in_word = true;
                loop {
                    match chars.next() {
                        Some('"') => break,
                        Some('\\') => match chars.next() {
                            Some(c @ ('"' | '\\' | '$' | '`')) => word.push(c),
                            Some(c) => {
                                word.push('\\');
                                word.push(c);
                            }
                            None => return Err(SplitError::UnclosedQuote),
                        },
                        Some(c) => word.push(c),
                        None => return Err(SplitError::UnclosedQuote),
                    }
                }
            }
            '\\' => {
                in_word = true;
                if let Some(c) = chars.next() {
                    word.push(c);
                }
            }
            c => {
                in_word = true;
                word.push(c);
            }
        }
    }
    if in_word {
        words.push(word);
    }
    if words.is_empty() {
        return Err(SplitError::Empty);
    }
    Ok(words)
}

/// The program and arguments that open `file` with `editor`, which is an app or a command line:
/// on macOS `open -a <bundle> <file>`, on Linux `<program> <file>`, and for a command line its
/// words with the file appended. `detected` is what detection found.
pub fn command_for(
    platform: Platform,
    editor: &Editor,
    detected: &[Detected],
    file: &Path,
) -> Result<Vec<String>, String> {
    let file = file.to_string_lossy().into_owned();
    match editor {
        Editor::System => Err("the system default opens through the opener plugin".to_owned()),
        Editor::App { id, name } => {
            let found = detected
                .iter()
                .find(|d| &d.id == id)
                .ok_or_else(|| format!("{name} was not found on this machine"))?;
            let launcher = found.launcher.to_string_lossy().into_owned();
            Ok(match platform {
                Platform::MacOs => {
                    vec!["/usr/bin/open".to_owned(), "-a".to_owned(), launcher, file]
                }
                Platform::Linux => vec![launcher, file],
            })
        }
        Editor::Custom { command } => {
            let mut words = split_command_line(command).map_err(|err| match err {
                SplitError::Empty => "the command is empty".to_owned(),
                SplitError::UnclosedQuote => {
                    "the command has a quote that is not closed".to_owned()
                }
            })?;
            words.push(file);
            Ok(words)
        }
    }
}

/// Where a skill's file may be opened from: the library and agentx home, as the CLI resolves them
/// from `env` (docs/spec/cli-contract.md): `AGENTX_LIBRARY`, else `$HOME/.agents/skills`, and
/// `AGENTX_HOME`, else `$HOME/.agentx`. An empty variable counts as unset; none without `HOME`.
pub fn scope_roots(env: &HashMap<String, String>) -> Vec<PathBuf> {
    let Some(home) = env.get("HOME").filter(|h| !h.is_empty()).map(PathBuf::from) else {
        return Vec::new();
    };
    let var = |name: &str| env.get(name).filter(|v| !v.is_empty()).map(PathBuf::from);
    vec![
        var("AGENTX_LIBRARY").unwrap_or_else(|| home.join(".agents/skills")),
        var("AGENTX_HOME").unwrap_or_else(|| home.join(".agentx")),
    ]
}

/// Whether `file` lies inside one of `roots`, both compared as their canonical paths, so that
/// neither `..` nor a symlink leads out of them: a fork's library entry, a symlink into its
/// worktree in agentx home, stays inside. A file or root that cannot be resolved is outside.
pub fn in_scope(file: &Path, roots: &[PathBuf]) -> bool {
    let Ok(file) = file.canonicalize() else {
        return false;
    };
    roots
        .iter()
        .filter_map(|root| root.canonicalize().ok())
        .any(|root| file != root && file.starts_with(&root))
}

/// How long a started editor is watched: one that exits within it with a failure did not open the
/// file. A launcher that hands the file to a running editor exits at once with success, and one
/// that keeps running, such as `code --wait`, is left to run.
const WATCH: Duration = Duration::from_secs(1);

/// Runs `words` with `env`, detached from the app: no input, its output dropped, in a process
/// group of its own. Fails when it cannot start, or exits with a failure within `WATCH`.
pub async fn launch(words: &[String], env: &HashMap<String, String>) -> Result<(), String> {
    let (program, args) = words.split_first().ok_or("nothing to run")?;
    let mut child = Command::new(program)
        .args(args)
        .env_clear()
        .envs(env)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .process_group(0)
        .spawn()
        .map_err(|err| format!("{program}: {err}"))?;
    match tokio::time::timeout(WATCH, child.wait()).await {
        Ok(Ok(status)) if status.success() => Ok(()),
        Ok(Ok(status)) => Err(format!("{program} exited with {status}")),
        Ok(Err(err)) => Err(format!("{program}: {err}")),
        Err(_) => {
            // Still running: wait for it elsewhere, so it does not stay a zombie when it ends.
            tokio::spawn(async move {
                let _ = child.wait().await;
            });
            Ok(())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn words(line: &str) -> Vec<String> {
        split_command_line(line).unwrap()
    }

    #[test]
    fn split_command_line_splits_like_a_shell_without_expanding() {
        let cases: &[(&str, &[&str])] = &[
            ("code --wait", &["code", "--wait"]),
            ("  code   -n  ", &["code", "-n"]),
            (
                "'/Applications/My Editor.app/bin/ed' -w",
                &["/Applications/My Editor.app/bin/ed", "-w"],
            ),
            ("\"my editor\" --flag", &["my editor", "--flag"]),
            ("ed \"a \\\"b\\\" c\"", &["ed", "a \"b\" c"]),
            ("ed \"\\n\"", &["ed", "\\n"]),
            ("my\\ editor -x", &["my editor", "-x"]),
            ("ed ''", &["ed", ""]),
            (
                "ed $HOME ~/x *.md `id` $(id)",
                &["ed", "$HOME", "~/x", "*.md", "`id`", "$(id)"],
            ),
            ("ed a;b|c", &["ed", "a;b|c"]),
            ("ed 'it''s'", &["ed", "its"]),
        ];
        for (line, want) in cases {
            assert_eq!(words(line), want.to_vec(), "{line}");
        }
    }

    #[test]
    fn split_command_line_refuses_an_empty_line_or_an_open_quote() {
        assert_eq!(split_command_line("   "), Err(SplitError::Empty));
        assert_eq!(split_command_line(""), Err(SplitError::Empty));
        assert_eq!(split_command_line("ed 'x"), Err(SplitError::UnclosedQuote));
        assert_eq!(split_command_line("ed \"x"), Err(SplitError::UnclosedQuote));
    }

    #[test]
    fn detect_finds_bundles_on_macos_in_either_applications_folder() {
        let present = [
            PathBuf::from("/Applications/Visual Studio Code.app"),
            PathBuf::from("/Users/me/Applications/Zed.app"),
        ];
        let found = detect(Platform::MacOs, Path::new("/Users/me"), None, |p| {
            present.iter().any(|q| q == p)
        });
        assert_eq!(
            found,
            vec![
                Detected {
                    id: "vscode".into(),
                    name: "VS Code".into(),
                    launcher: present[0].clone(),
                },
                Detected {
                    id: "zed".into(),
                    name: "Zed".into(),
                    launcher: present[1].clone(),
                },
            ]
        );
    }

    #[test]
    fn detect_finds_programs_on_linux_in_path_order() {
        let present = [
            PathBuf::from("/usr/bin/subl"),
            PathBuf::from("/home/me/.local/bin/zeditor"),
            PathBuf::from("/usr/bin/cursor"),
            PathBuf::from("/home/me/.local/bin/cursor"),
        ];
        let found = detect(
            Platform::Linux,
            Path::new("/home/me"),
            Some("/home/me/.local/bin::/usr/bin"),
            |p| present.iter().any(|q| q == p),
        );
        let ids: Vec<_> = found
            .iter()
            .map(|d| (d.id.as_str(), d.launcher.clone()))
            .collect();
        assert_eq!(
            ids,
            vec![
                ("cursor", PathBuf::from("/home/me/.local/bin/cursor")),
                ("zed", PathBuf::from("/home/me/.local/bin/zeditor")),
                ("sublime", PathBuf::from("/usr/bin/subl")),
            ]
        );
        assert!(detect(Platform::Linux, Path::new("/home/me"), None, |_| true).is_empty());
    }

    #[test]
    fn command_for_opens_the_file_in_the_app_or_appends_it_to_the_command() {
        let file = Path::new("/home/me/.agents/skills/my-skill/SKILL.md");
        let code = Detected {
            id: "vscode".into(),
            name: "VS Code".into(),
            launcher: PathBuf::from("/usr/bin/code"),
        };
        let app = Editor::App {
            id: "vscode".into(),
            name: "VS Code".into(),
        };
        assert_eq!(
            command_for(Platform::Linux, &app, std::slice::from_ref(&code), file).unwrap(),
            ["/usr/bin/code", file.to_str().unwrap()]
        );
        let bundle = Detected {
            launcher: PathBuf::from("/Applications/Visual Studio Code.app"),
            ..code
        };
        assert_eq!(
            command_for(Platform::MacOs, &app, &[bundle], file).unwrap(),
            [
                "/usr/bin/open",
                "-a",
                "/Applications/Visual Studio Code.app",
                file.to_str().unwrap()
            ]
        );
        assert_eq!(
            command_for(Platform::Linux, &app, &[], file).unwrap_err(),
            "VS Code was not found on this machine"
        );
        let custom = Editor::Custom {
            command: "subl -n".into(),
        };
        assert_eq!(
            command_for(Platform::Linux, &custom, &[], file).unwrap(),
            ["subl", "-n", file.to_str().unwrap()]
        );
        let empty = Editor::Custom {
            command: " ".into(),
        };
        assert!(command_for(Platform::Linux, &empty, &[], file).is_err());
        assert!(command_for(Platform::Linux, &Editor::System, &[], file).is_err());
    }

    #[test]
    fn scope_roots_follow_the_variables_the_cli_reads() {
        let env = |pairs: &[(&str, &str)]| -> HashMap<String, String> {
            pairs
                .iter()
                .map(|(k, v)| ((*k).to_owned(), (*v).to_owned()))
                .collect()
        };
        assert_eq!(
            scope_roots(&env(&[("HOME", "/home/me")])),
            [
                PathBuf::from("/home/me/.agents/skills"),
                PathBuf::from("/home/me/.agentx")
            ]
        );
        assert_eq!(
            scope_roots(&env(&[
                ("HOME", "/home/me"),
                ("AGENTX_LIBRARY", "/data/skills"),
                ("AGENTX_HOME", "")
            ])),
            [
                PathBuf::from("/data/skills"),
                PathBuf::from("/home/me/.agentx")
            ]
        );
        assert!(scope_roots(&env(&[("AGENTX_HOME", "/x")])).is_empty());
    }

    #[test]
    fn in_scope_compares_canonical_paths() {
        let dir = std::env::temp_dir().join(format!("agentx-scope-{}", std::process::id()));
        let library = dir.join("library");
        let home = dir.join("home");
        let outside = dir.join("outside");
        for d in [
            library.join("mine"),
            home.join("worktrees/fork/fork"),
            outside.clone(),
        ] {
            std::fs::create_dir_all(d).unwrap();
        }
        for f in [
            library.join("mine/SKILL.md"),
            home.join("worktrees/fork/fork/SKILL.md"),
            outside.join("SKILL.md"),
        ] {
            std::fs::write(f, "x").unwrap();
        }
        // A fork's library entry leads into agentx home; another leads out of both.
        std::os::unix::fs::symlink(home.join("worktrees/fork/fork"), library.join("fork")).unwrap();
        std::os::unix::fs::symlink(&outside, library.join("away")).unwrap();
        let roots = [library.clone(), home.clone()];

        assert!(in_scope(&library.join("mine/SKILL.md"), &roots));
        assert!(in_scope(&library.join("fork/SKILL.md"), &roots));
        assert!(!in_scope(&outside.join("SKILL.md"), &roots));
        assert!(!in_scope(&library.join("../outside/SKILL.md"), &roots));
        assert!(!in_scope(&library.join("away/SKILL.md"), &roots));
        assert!(!in_scope(&library.join("missing/SKILL.md"), &roots));
        assert!(!in_scope(&library, &roots));
        assert!(!in_scope(&library.join("mine/SKILL.md"), &[]));

        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[tokio::test]
    async fn launch_reports_a_program_that_fails_or_is_missing() {
        let env: HashMap<String, String> = [("PATH".to_owned(), "/usr/bin:/bin".to_owned())].into();
        assert!(launch(&["true".to_owned()], &env).await.is_ok());
        assert!(launch(&["false".to_owned()], &env).await.is_err());
        assert!(launch(&["agentx-no-such-editor".to_owned()], &env)
            .await
            .is_err());
        // One that keeps running is left to run.
        assert!(launch(&["sleep".to_owned(), "3".to_owned()], &env)
            .await
            .is_ok());
    }
}
