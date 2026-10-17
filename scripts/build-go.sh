#!/usr/bin/env bash
# Builds the Go engine into app/libs/netmerge.aar with gomobile.
# Needs Go, the Android SDK (ANDROID_HOME) and an NDK (ANDROID_NDK_HOME).
set -euo pipefail
cd "$(dirname "$0")/../golib"
# gomobile and gobind at the versions pinned in go.mod.
GOBIN="$PWD/.bin" go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
export PATH="$PWD/.bin:$PATH"
targets="${GOMOBILE_TARGETS:-android/arm64,android/arm,android/amd64}"
mkdir -p ../app/libs
gomobile bind -v -trimpath -ldflags="-s -w" \
  -target="$targets" -androidapi 26 \
  -o ../app/libs/netmerge.aar ./netmerge
