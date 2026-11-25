// The app's preferences (ADR 0013): what the desktop app alone decides on this machine, today the
// editor a skill opens in, whether it opens a skill the app just created, and whether the app
// checks for its own updates (ADR 0019). They are one JSON file, `preferences.json`, in the app's
// config directory, which the app reads and writes itself. They are not machine settings: the CLI
// never reads them, and a snapshot does not carry them.

use std::io;
use std::path::Path;

use serde::{Deserialize, Serialize};

use crate::editor::Editor;

/// The file's name in the app's config directory.
pub const FILE_NAME: &str = "preferences.json";

/// Everything the file holds. A field missing from the file takes its default.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Preferences {
    #[serde(default)]
    pub editor: Editor,
    /// Whether the editor opens a skill once the app creates it. Off by default.
    #[serde(default)]
    pub open_after_create: bool,
    /// Whether the app checks for its own updates after it starts and once a day. On by default.
    #[serde(default = "on")]
    pub check_updates: bool,
}

const fn on() -> bool {
    true
}

impl Default for Preferences {
    fn default() -> Self {
        Self {
            editor: Editor::default(),
            open_after_create: false,
            check_updates: true,
        }
    }
}

/// The preferences in `file`, or the defaults when it is missing or cannot be read as preferences.
pub fn read(file: &Path) -> Preferences {
    std::fs::read(file)
        .ok()
        .and_then(|bytes| serde_json::from_slice(&bytes).ok())
        .unwrap_or_default()
}

/// Writes `preferences` to `file` whole: to a file beside it first, then renamed over it, so a
/// crash never leaves half a file.
pub fn write(file: &Path, preferences: &Preferences) -> io::Result<()> {
    if let Some(dir) = file.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let mut bytes = serde_json::to_vec_pretty(preferences).map_err(io::Error::other)?;
    bytes.push(b'\n');
    let temporary = file.with_extension("json.tmp");
    std::fs::write(&temporary, bytes)?;
    std::fs::rename(&temporary, file)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preferences_round_trip_and_default_when_missing_or_unreadable() {
        let dir = std::env::temp_dir().join(format!("agentx-preferences-{}", std::process::id()));
        let file = dir.join("nested").join(FILE_NAME);
        assert_eq!(read(&file), Preferences::default());

        let custom = Preferences {
            editor: Editor::Custom {
                command: "code --wait".into(),
            },
            open_after_create: true,
            check_updates: false,
        };
        write(&file, &custom).unwrap();
        assert_eq!(read(&file), custom);
        let written = std::fs::read_to_string(&file).unwrap();
        assert!(written.contains("\"kind\": \"custom\""), "{written}");
        assert!(written.contains("\"check_updates\": false"), "{written}");
        assert!(written.contains("\"open_after_create\": true"), "{written}");

        std::fs::write(&file, b"{\"editor\":{\"kind\":\"vim\"}}").unwrap();
        assert_eq!(read(&file), Preferences::default());
        std::fs::write(&file, b"{}").unwrap();
        assert_eq!(read(&file), Preferences::default());

        std::fs::remove_dir_all(&dir).unwrap();
    }
}
