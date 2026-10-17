#!/bin/sh
set -eu

fail() {
  printf 'generate-homebrew-formula: %s\n' "$*" >&2
  exit 1
}

version=${1:-}
checksums=${2:-}
[ -n "$version" ] && [ -n "$checksums" ] || fail 'usage: scripts/generate-homebrew-formula.sh VERSION checksums.txt'
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail "invalid stable version: $version"
[ -f "$checksums" ] || fail "checksum file not found: $checksums"
[ "$(wc -l < "$checksums" | tr -d ' ')" = 4 ] || fail 'checksums.txt must contain exactly four platform assets'

checksum_for() {
  asset=$1
  awk -v wanted="$asset" '
    NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-f]/ { invalid = 1 }
    $2 == wanted { count++; checksum = $1 }
    END {
      if (invalid || count != 1) exit 1
      print checksum
    }
  ' "$checksums"
}

linux_x64=$(checksum_for "noon-v${version}-linux-x64.tar.gz") || fail 'missing, duplicated, or invalid Linux x64 checksum'
linux_arm64=$(checksum_for "noon-v${version}-linux-arm64.tar.gz") || fail 'missing, duplicated, or invalid Linux arm64 checksum'
macos_x64=$(checksum_for "noon-v${version}-macos-x64.tar.gz") || fail 'missing, duplicated, or invalid macOS x64 checksum'
macos_arm64=$(checksum_for "noon-v${version}-macos-arm64.tar.gz") || fail 'missing, duplicated, or invalid macOS arm64 checksum'

cat <<EOF
class Noon < Formula
  desc "Local-first repository job scheduler"
  homepage "https://github.com/brettinternet/noon"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/brettinternet/noon/releases/download/v${version}/noon-v${version}-macos-arm64.tar.gz"
      sha256 "${macos_arm64}"
    end
    on_intel do
      url "https://github.com/brettinternet/noon/releases/download/v${version}/noon-v${version}-macos-x64.tar.gz"
      sha256 "${macos_x64}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/brettinternet/noon/releases/download/v${version}/noon-v${version}-linux-arm64.tar.gz"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "https://github.com/brettinternet/noon/releases/download/v${version}/noon-v${version}-linux-x64.tar.gz"
      sha256 "${linux_x64}"
    end
  end

  def install
    # Homebrew strips the archive's single top-level bin directory.
    bin.install "noon"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/noon --version")

    home = testpath/"home"
    config = testpath/"config"
    state = testpath/"noon-state"
    home.mkpath
    config.mkpath
    ENV["HOME"] = home.to_s
    ENV["XDG_CONFIG_HOME"] = config.to_s
    ENV["XDG_STATE_HOME"] = (testpath/"xdg-state").to_s
    ENV["NOON_STATE_DIR"] = state.to_s
    project = testpath/"project"
    project.mkpath
    (project/"noon.yaml").write <<~YAML
      version: 1
      jobs:
        homebrew-smoke:
          schedule: every day at 2am
          run: [sh, -c, "printf ran > homebrew-smoke-job-ran"]
    YAML
    ENV["NOON_PROJECT"] = project.to_s
    system bin/"noon"
    refute((project/"homebrew-smoke-job-ran").exist?)
    refute(state.exist?)
    refute((home/"Library/LaunchAgents/io.github.brettinternet.noon.plist").exist?)
    refute((config/"systemd/user/noon.service").exist?)
  end
end
EOF
