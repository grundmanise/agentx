// A log file that rotates by size: `<name>` is written, and when the next line would take it over
// the limit it becomes `<name>.1`, the older ones move up one and the oldest is removed.

use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};

pub struct RotatingLog {
    dir: PathBuf,
    name: String,
    max_bytes: u64,
    files: usize,
    file: Option<File>,
    size: u64,
}

impl RotatingLog {
    /// A log of at most `files` files of `max_bytes` each. Nothing is created before the first
    /// line.
    pub fn new(dir: PathBuf, name: &str, max_bytes: u64, files: usize) -> Self {
        RotatingLog {
            dir,
            name: name.to_owned(),
            max_bytes,
            files: files.max(1),
            file: None,
            size: 0,
        }
    }

    /// Appends one line. A log that cannot be written loses the line: logging never stops the
    /// child whose output it keeps.
    pub fn write_line(&mut self, line: &str) {
        let len = line.len() as u64 + 1;
        if self.file.is_some() && self.size > 0 && self.size + len > self.max_bytes {
            self.rotate();
        }
        if self.file.is_none() {
            self.open();
        }
        if let Some(file) = &mut self.file {
            if writeln!(file, "{line}").is_ok() {
                self.size += len;
            }
        }
    }

    fn path(&self, n: usize) -> PathBuf {
        if n == 0 {
            self.dir.join(&self.name)
        } else {
            self.dir.join(format!("{}.{n}", self.name))
        }
    }

    fn open(&mut self) {
        if fs::create_dir_all(&self.dir).is_err() {
            return;
        }
        let path = self.path(0);
        self.file = append(&path);
        self.size = fs::metadata(&path).map_or(0, |m| m.len());
        if self.size >= self.max_bytes {
            self.rotate();
        }
    }

    fn rotate(&mut self) {
        self.file = None;
        let _ = fs::remove_file(self.path(self.files - 1));
        for n in (0..self.files - 1).rev() {
            let _ = fs::rename(self.path(n), self.path(n + 1));
        }
        self.file = append(&self.path(0));
        self.size = 0;
    }
}

fn append(path: &Path) -> Option<File> {
    OpenOptions::new().create(true).append(true).open(path).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rotates_by_size_and_keeps_the_newest_files() {
        let dir = std::env::temp_dir().join(format!("agentx-logfile-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        let mut log = RotatingLog::new(dir.clone(), "serve.log", 10, 3);
        for line in ["aaaa", "bbbb", "cccc", "dddd", "eeee"] {
            log.write_line(line);
        }
        let read = |name: &str| fs::read_to_string(dir.join(name)).unwrap();
        assert_eq!(read("serve.log"), "eeee\n");
        assert_eq!(read("serve.log.1"), "cccc\ndddd\n");
        assert_eq!(read("serve.log.2"), "aaaa\nbbbb\n");
        assert!(!dir.join("serve.log.3").exists());
        fs::remove_dir_all(&dir).unwrap();
    }
}
