// Reports a panic of the Rust side to PostHog, under the same `telemetry` setting as the app
// (docs/help-center/telemetry.mdx). The app calls `set_telemetry` each time it learns the setting,
// with the host and token it sends to itself. The first call with the setting on starts the
// PostHog SDK, whose own panic hook then sends each panic as an `$exception` event, after the
// panic message and before the panic goes on. While the setting is off, `prepare` drops every
// event; once started, the SDK stays started until the app quits. An event leaves tagged with the
// release channel and `surface: "app"`, and with every user path in it replaced by `<path>`, as
// the CLI and the app do.

use std::sync::{Mutex, OnceLock};

use posthog::{ClientOptionsBuilder, ErrorTrackingOptionsBuilder, Event};
use serde_json::Value;

/// What the app tells the Rust side about its usage data.
#[derive(Clone, Debug, Default, serde::Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Telemetry {
    /// Whether the setting is on and this build sends at all.
    pub on: bool,
    /// `nightly` or `stable`; absent in a build that is no release.
    pub release_channel: String,
    /// The PostHog ingestion host and project token the app sends to.
    pub host: String,
    pub token: String,
}

/// What the app last said; `None` until it says anything.
static CURRENT: Mutex<Option<Telemetry>> = Mutex::new(None);
/// Set once the SDK has started.
static STARTED: OnceLock<()> = OnceLock::new();

/// Takes the app's usage data setting, and starts the SDK the first time it is on.
#[tauri::command(rename_all = "snake_case")]
pub async fn set_telemetry(telemetry: Telemetry) -> Result<(), String> {
    let start = telemetry.on && STARTED.get().is_none();
    let options = start.then(|| options(&telemetry)).transpose()?;
    if let Ok(mut current) = CURRENT.lock() {
        *current = Some(telemetry);
    }
    if let Some(options) = options {
        if STARTED.set(()).is_ok() {
            posthog::init_global(options)
                .await
                .map_err(|err| err.to_string())?;
        }
    }
    Ok(())
}

fn options(telemetry: &Telemetry) -> Result<posthog::ClientOptions, String> {
    let error_tracking = ErrorTrackingOptionsBuilder::default()
        .capture_panics(true)
        .build()
        .map_err(|err| err.to_string())?;
    ClientOptionsBuilder::default()
        .api_key(telemetry.token.clone())
        .host(telemetry.host.clone())
        .error_tracking(error_tracking)
        .before_send(|event| {
            let current = CURRENT.lock().ok()?.clone()?;
            prepare(event, &current)
        })
        .build()
        .map_err(|err| err.to_string())
}

/// `event` as it leaves: nothing while the setting is off, else with the release channel and the
/// surface added and every user path replaced by `<path>`.
fn prepare(mut event: Event, telemetry: &Telemetry) -> Option<Event> {
    if !telemetry.on {
        return None;
    }
    let keys: Vec<String> = event.properties().keys().cloned().collect();
    for key in keys {
        if let Some(value) = event.remove_prop(&key) {
            event.insert_prop(key, redact_all(value)).ok()?;
        }
    }
    event
        .insert_prop("release_channel", &telemetry.release_channel)
        .ok()?;
    event.insert_prop("surface", "app").ok()?;
    Some(event)
}

/// `value` with every string in it, however deep, passed through `redact_paths`.
fn redact_all(value: Value) -> Value {
    match value {
        Value::String(text) => Value::String(redact_paths(&text)),
        Value::Array(items) => Value::Array(items.into_iter().map(redact_all).collect()),
        Value::Object(map) => Value::Object(
            map.into_iter()
                .map(|(key, value)| (key, redact_all(value)))
                .collect(),
        ),
        other => other,
    }
}

/// Where a user's or the system's temporary files live.
const USER_ROOTS: [&str; 6] = [
    "/Users",
    "/home",
    "/root",
    "/private",
    "/tmp",
    "/var/folders",
];

/// `text` with every absolute path under `USER_ROOTS` replaced by `<path>`, up to the next space,
/// quote, bracket or colon, so a location keeps its line and column. The app's `redactPaths` uses
/// the same rule.
fn redact_paths(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    while let Some((at, root)) = next_root(rest) {
        out.push_str(&rest[..at]);
        let after = &rest[at + root.len()..];
        let end = after
            .find(|c: char| c.is_whitespace() || "\"'`<>()[]{},;:".contains(c))
            .unwrap_or(after.len());
        out.push_str("<path>");
        rest = &after[end..];
    }
    out.push_str(rest);
    out
}

/// The first place in `text` where a user root starts a path: followed by `/` or by a character
/// that cannot go on a name, so `/homework` is not one.
fn next_root(text: &str) -> Option<(usize, &'static str)> {
    USER_ROOTS
        .iter()
        .flat_map(|root| text.match_indices(root).map(move |(at, _)| (at, *root)))
        .filter(|(at, root)| {
            text[at + root.len()..]
                .chars()
                .next()
                .is_none_or(|c| c == '/' || !(c.is_alphanumeric() || "_.-".contains(c)))
        })
        .min_by_key(|(at, root)| (*at, std::cmp::Reverse(root.len())))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn on() -> Telemetry {
        Telemetry {
            on: true,
            release_channel: "nightly".into(),
            ..Telemetry::default()
        }
    }

    #[test]
    fn replaces_user_paths_and_keeps_line_and_column() {
        assert_eq!(
            redact_paths("failed to read /Users/someone/.agentx/settings.json: denied"),
            "failed to read <path>: denied"
        );
        assert_eq!(redact_paths("at /home/someone/x.rs:12:5"), "at <path>:12:5");
        assert_eq!(
            redact_paths("'/var/folders/ab/T/x' and /tmp"),
            "'<path>' and <path>"
        );
        assert_eq!(
            redact_paths("src/lib.rs and /homework"),
            "src/lib.rs and /homework"
        );
    }

    #[test]
    fn redacts_every_string_and_tags_the_event() {
        let mut event = Event::new_anon("$exception");
        event
            .insert_prop(
                "$exception_list",
                serde_json::json!([{ "value": "open /home/someone/a: denied", "frames": [{ "filename": "/root/b.rs" }] }]),
            )
            .unwrap();
        let event = prepare(event, &on()).unwrap();
        let props = event.properties();
        assert_eq!(props["$exception_list"][0]["value"], "open <path>: denied");
        assert_eq!(
            props["$exception_list"][0]["frames"][0]["filename"],
            "<path>"
        );
        assert_eq!(props["release_channel"], "nightly");
        assert_eq!(props["surface"], "app");
    }

    /// Answers one HTTP request on `listener` and returns its body.
    fn answer_one(listener: std::net::TcpListener) -> String {
        use std::io::{Read, Write};
        let (mut stream, _) = listener.accept().unwrap();
        let mut seen = Vec::new();
        let mut buf = [0u8; 8192];
        loop {
            let n = stream.read(&mut buf).unwrap();
            seen.extend_from_slice(&buf[..n]);
            let text = String::from_utf8_lossy(&seen);
            if let Some(end) = text.find("\r\n\r\n") {
                let length = text[..end]
                    .lines()
                    .find_map(|line| {
                        let (name, value) = line.split_once(':')?;
                        name.eq_ignore_ascii_case("content-length")
                            .then(|| value.trim().parse::<usize>().ok())?
                    })
                    .unwrap_or(0);
                if seen.len() >= end + 4 + length {
                    let body = String::from_utf8_lossy(&seen[end + 4..]).into_owned();
                    let answer = "{\"status\":\"Ok\"}";
                    let _ = write!(
                        stream,
                        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{answer}",
                        answer.len()
                    );
                    return body;
                }
            }
        }
    }

    /// The whole path: the app turns the setting on, a thread panics, and the SDK's own panic
    /// hook sends the `$exception` event through `prepare`. The hook is process-wide, so this is
    /// the one test that starts the SDK.
    #[test]
    fn sends_a_panic_through_the_sdk_once_the_setting_is_on() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let host = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || answer_one(listener));
        tauri::async_runtime::block_on(set_telemetry(Telemetry {
            on: true,
            release_channel: "stable".into(),
            host,
            token: "phc_test".into(),
        }))
        .unwrap();

        let _ = std::thread::spawn(|| panic!("cannot read /Users/someone/.agentx/settings.json"))
            .join();

        let body = server.join().unwrap();
        assert!(body.contains("\"$exception\""), "{body}");
        assert!(body.contains("cannot read <path>"), "{body}");
        assert!(!body.contains("someone"), "{body}");
        assert!(body.contains("\"release_channel\":\"stable\""), "{body}");
        assert!(body.contains("\"surface\":\"app\""), "{body}");
    }

    #[test]
    fn drops_every_event_while_the_setting_is_off() {
        let off = Telemetry { on: false, ..on() };
        assert!(prepare(Event::new_anon("$exception"), &off).is_none());
    }
}
