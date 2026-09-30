# Template for Formula/agentx.rb in grundmanise/homebrew-tap. On every stable release, the
# release workflow fills in the version and checksum placeholders below and pushes the result.
class Agentx < Formula
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
      system "go", "build", *std_go_args(ldflags:)
    end
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/agentx version")
  end
end
