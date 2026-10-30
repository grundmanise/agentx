# Source: packaging/homebrew/agentx.cask.rb in grundmanise/agentx. Each stable release whose app
# was notarized fills in the version and the checksum of the disk image and copies it to
# Casks/agentx.rb in grundmanise/homebrew-tap. Edit the source, not the copy in the tap: the next
# release overwrites the copy.
#
# The desktop app for Apple silicon Macs, with the CLI of the same release inside it, which the
# cask puts on PATH as agentx. The CLI alone is the formula `agentx-cli`.
cask "agentx" do
  version "@VERSION@"
  sha256 "@SHA256@"

  url "https://github.com/grundmanise/agentx/releases/download/v#{version}/agentx_desktop_darwin_arm64.dmg"
  name "agentx"
  desc "Inventory manager for your agents' skills, MCP servers and plugins"
  homepage "https://docs.agentx.wtf"

  livecheck do
    url "https://github.com/grundmanise/agentx/releases/latest"
    strategy :github_latest
  end

  depends_on arch: :arm64
  # agentx runs git 2.40 or later, newer than some macOS versions ship.
  depends_on formula: "git"
  # The formula agentx-cli also links agentx, so Homebrew refuses to install this cask next to it.

  app "agentx.app"
  binary "#{appdir}/agentx.app/Contents/MacOS/agentx"

  # Only the app's own files. ~/.agentx holds the CLI's data, which stays.
  zap trash: [
    "~/Library/Application Support/dev.agentx.desktop",
    "~/Library/Caches/dev.agentx.desktop",
    "~/Library/HTTPStorages/dev.agentx.desktop",
    "~/Library/Logs/agentx",
    "~/Library/Preferences/dev.agentx.desktop.plist",
    "~/Library/Saved Application State/dev.agentx.desktop.savedState",
    "~/Library/WebKit/dev.agentx.desktop",
  ]
end
