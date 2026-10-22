# Hestia

Local control for Sonoff switches running stock eWeLink firmware. No cloud, no account, no flashing.

![Hestia control screen](docs/screenshot-control.png)

Hestia is a single Windows program. It finds Sonoff switches on your Wi-Fi, reads each switch's local encryption key once, and then sends on/off commands directly to the switch over your network. It never contacts eWeLink or any other internet service.

## How it works

| Step | What Hestia does |
| --- | --- |
| **1. Test** | Listens for switches announcing local control (mDNS `_ewelink._tcp`) and checks it can reach them on port 8081. Changes nothing. |
| **2. Setup** | With the switch in setup mode and the PC on its `ITEAD-10000…` hotspot, reads the device key from `http://10.10.7.1/device`. |
| **3. Control** | Unlocks once the test passes and the key is confirmed. On/off button, live state, Ctrl+Alt keyboard shortcuts. |
| **Activity** | Logs every command, drop-off and reconnection, for stability testing with the cloud blocked. Exportable. |

Commands use the eWeLink LAN protocol: an HTTP POST to `/zeroconf/switch` whose payload is AES-128-CBC encrypted with `MD5(device key)` and a fresh random IV. Switch state is read from the encrypted mDNS TXT records.

Step-by-step instructions for a first run: [docs/HOW-TO.md](docs/HOW-TO.md).

## Security

- The UI is served on `127.0.0.1` only, behind a random per-run token, with a Host-header check against DNS rebinding and a strict Content-Security-Policy.
- Device keys are encrypted with Windows DPAPI for the current user and never written to the log.
- Hestia refuses to send commands to anything outside private, link-local or loopback address ranges.
- The release build contains no test or debug overrides (those exist only behind the `dev` build tag).

## Build

Requires Go 1.24 or newer, on any OS:

```sh
./build.sh          # produces dist/Hestia.exe and prints its SHA-256
```

## Testing without hardware

`cmd/fakedevice` simulates a Sonoff switch in LAN mode (mDNS announcements, encrypted commands, setup-mode key endpoint):

```sh
go run ./cmd/fakedevice -iface eth0 &
HESTIA_SETUP_HOST=127.0.0.1:8090 go run -tags dev . -no-window
```

Use `HESTIA_DEV_NET=<cidr>` (dev builds only) if your test network isn't a private range.

## Status

Version 1.0.0. Tested against the simulator and under Wine. Not yet verified on a real Sonoff RFR2.
