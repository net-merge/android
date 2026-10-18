#!/usr/bin/env bash
# Takes the README screenshots on a running emulator: installs the debug APK, switches merging
# on without the VPN prompt, pushes some traffic through it and captures light and dark shots.
# Usage: scripts/screenshots.sh path/to/app-debug.apk docs/screenshots
set -uo pipefail
APK="$1"
OUT="$2"
PKG=io.github.senseiredsmoke.netmerge
mkdir -p "$OUT"

adb wait-for-device
# A freshly booted emulator's launcher often throws an "isn't responding" dialog; keep error
# dialogs out of the shots and give the system time to settle.
adb shell settings put global hide_error_dialogs 1 || true
sleep 20
adb shell input keyevent KEYCODE_HOME
adb install -r "$APK"
adb shell appops set "$PKG" ACTIVATE_VPN allow
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS || true
adb shell svc wifi enable || true
adb shell svc data enable || true

# A clean status bar.
adb shell settings put global sysui_demo_allowed 1
demo() { adb shell am broadcast -a com.android.systemui.demo -e command "$@" > /dev/null; }
demo enter
demo clock -e hhmm 0941
demo battery -e level 92 -e plugged false
demo notifications -e visible false

shot() { adb exec-out screencap -p > "$OUT/$1.png"; echo "captured $1"; }

adb shell cmd uimode night yes
adb shell am start -S -W -n "$PKG/.MainActivity"
sleep 5
shot off-dark

adb shell am start -S -W -n "$PKG/.MainActivity" --ez autostart true
sleep 8
# Several parallel downloads from the emulator shell, which goes through the VPN like any app.
for i in 1 2 3 4 5 6; do
  adb shell "printf 'GET /100MB.zip HTTP/1.0\r\nHost: speedtest.tele2.net\r\n\r\n' | toybox nc speedtest.tele2.net 80 > /dev/null 2>&1 &" &
done
sleep 25
shot on-dark
adb shell cmd uimode night no
sleep 4
shot on-light
adb shell dumpsys connectivity | grep -E "NetworkAgentInfo" | head -5 || true
ls -la "$OUT"
