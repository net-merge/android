# Net Merge for Android

Use Wi-Fi and mobile data at the same time. Net Merge for Android spreads your phone's
connections over both networks, so things that open many connections at once (browsers,
multi-stream downloads, torrents, app stores) get the combined speed. No root needed.

<p align="center">
  <img src="docs/screenshots/on-dark.png" width="30%" alt="Merging Wi-Fi and mobile data, dark theme">
  <img src="docs/screenshots/on-light.png" width="30%" alt="Merging, light theme">
  <img src="docs/screenshots/off-dark.png" width="30%" alt="Switched off">
</p>

<p align="center">
  <a href="https://github.com/SenseiRedsmoke/net-merge-android/releases/latest/download/net-merge-android.apk"><b>Download the APK</b></a>
</p>

This is an Android port of [net-merge](https://github.com/rohit267/net-merge) by Rohit Mahto
(MIT). The balancing engine (scheduler, balancer, internet probes, IPv6 policy) is upstream's Go
code, used unchanged; this repository adds the Android plumbing around it.

## How it works

- The app is a local VPN (`VpnService`). Nothing leaves your phone through a remote server: the
  TUN is terminated on the phone in a userland TCP/IP stack (gVisor, via
  [tun2socks](https://github.com/xjasonlyu/tun2socks)).
- Every new TCP connection or UDP session is re-dialed by the engine over Wi-Fi or mobile data.
  The engine picks the network with the fewest active flows relative to its measured speed, and
  stops using a network whose internet check fails.
- Each outgoing socket is pinned to its network with `android_setsocknetwork` (the NDK form of
  `Network.bindSocket`). The app excludes itself from its own VPN so these sockets go straight out.
- Android normally turns mobile data off while Wi-Fi is connected. The app holds a
  `ConnectivityManager.requestNetwork` for both Wi-Fi and cellular so both stay up while it runs.
- Networks are discovered and updated live through `NetworkCallback` (Wi-Fi drops, mobile data
  comes back, dual SIM).

A single connection always uses one network, so one plain download or a speed test with one
stream won't get faster. Many parallel connections will. Upload works the same way: it only adds
up when an app uploads over several connections and both networks have a usable uplink. While
traffic is mostly upload, new connections are balanced by each network's measured upload speed.

## Using it

1. On your phone, open the [APK download link](https://github.com/SenseiRedsmoke/net-merge-android/releases/latest/download/net-merge-android.apk)
   and install it (allow "Install unknown apps" for your browser if Android asks).
2. Open Net Merge, tap the power button and accept the VPN prompt.
3. Optional: add the Net Merge tile to Quick Settings.

The screen shows each network's share of the traffic, its download and upload speed, and a live
chart of the last minute. **Split evenly** gives both networks the same share of new connections
instead of following their measured speed.

Mobile data stays on while merging is active, so keep an eye on your data plan.

## Building

Requirements: Go 1.27+, JDK 17, the Android SDK and an NDK.

```sh
export ANDROID_HOME=~/Android/Sdk
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/<version>
./scripts/build-go.sh          # Go engine -> app/libs/netmerge.aar (gomobile)
./gradlew assembleDebug        # -> app/build/outputs/apk/debug/
```

Engine tests run on any desktop: `cd golib && go test ./...`.

## Layout

| Path | What |
| --- | --- |
| `golib/internal/engine` | Upstream engine, copied from net-merge at `6093879`. Android additions: closing flows when their network disappears, and upload-aware weights |
| `golib/internal/tun` | TUN front-end: upstream `forward.go` plus an Android version of `tun.go` that takes the VPN's file descriptor |
| `golib/internal/nics` | Android replacement for upstream's adapter layer: socket pinning by network handle |
| `golib/netmerge` | The API the app calls through gomobile (`start`, `stop`, `setNetworks`, `stats`) |
| `app/` | Kotlin app: VPN service, network tracking, main screen, Quick Settings tile |

## License

MIT, same as upstream. See [LICENSE](LICENSE).
