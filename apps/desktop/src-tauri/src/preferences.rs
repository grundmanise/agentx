// The app's preferences (ADR 0013): what the desktop app alone decides on this machine, today the
// editor a skill opens in. They are one JSON file, `preferences.json`, in the app's config
// directory, which the app reads and writes itself. They are not machine settings: the CLI never
// reads them, and an export does not carry them.

use std::io;
use std::path::Path;

use serde::{Deserialize, Serialize};

use crate::editor::Editor;

/// The file's name in the app's config directory.
pub const FILE_NAME: &str = "preferences.json";

/// Everything the file holds. A field missing from the file takes its default.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Preferences {
    #[serde(default)]
    pub editor: Editor,
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
        };
        write(&file, &custom).unwrap();
        assert_eq!(read(&file), custom);
        let written = std::fs::read_to_string(&file).unwrap();
        assert!(written.contains("\"kind\": \"custom\""), "{written}");

        std::fs::write(&file, b"{\"editor\":{\"kind\":\"vim\"}}").unwrap();
        assert_eq!(read(&file), Preferences::default());
        std::fs::write(&file, b"{}").unwrap();
        assert_eq!(read(&file), Preferences::default());

        std::fs::remove_dir_all(&dir).unwrap();
    }
}
