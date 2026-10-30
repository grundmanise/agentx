# Source: packaging/homebrew/agentx-cli.rb in grundmanise/agentx. Each stable release fills in the
# version and checksum and copies it to Formula/agentx-cli.rb in grundmanise/homebrew-tap.
# Edit the source, not the copy in the tap: the next release overwrites the copy.
#
# The CLI alone, built from source, for macOS and Linux. The desktop app with its CLI is the cask
# `agentx` in packaging/homebrew/agentx.cask.rb, for macOS only.
class AgentxCli < Formula
  desc "Inventory manager for your agents' skills, MCP servers and plugins"
  homepage "https://docs.agentx.wtf"
  url "https://github.com/grundmanise/agentx/archive/refs/tags/v@VERSION@.tar.gz"
  sha256 "@SHA256@"
  license "Apache-2.0"
  head "https://github.com/grundmanise/agentx.git", branch: "main"

  depends_on "go" => :build
  # agentx runs git 2.40 or later, newer than some macOS versions ship.
  depends_on "git"

  def install
    # Static on Linux; cgo on macOS, where agentx serve watches through FSEvents.
    ENV["CGO_ENABLED"] = OS.mac? ? "1" : "0"
    ldflags = "-s -w -X github.com/grundmanise/agentx/apps/cli/internal/cli.cliVersion=#{version}"
    cd "apps/cli" do
      # The command is agentx, whatever the formula is called.
      system "go", "build", *std_go_args(ldflags:, output: bin/"agentx")
    end
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/agentx version")
  end
end
