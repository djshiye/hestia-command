# Hestia

**Control your Sonoff switches from your PC, over your own network. No cloud, no eWeLink account, no changes to the switch.**

![Hestia control screen](docs/screenshot-control.png)

<img src="docs/screenshot-tray.png" alt="Tray popup" width="340" align="right">

---

## How to install

> **[⬇ Download Hestia-Setup.exe](https://github.com/djshiye/hestia-command/releases/latest/download/Hestia-Setup.exe)**
> &nbsp;Windows 10 or 11, 64-bit · free · about 8 MB

1. **Download** `Hestia-Setup.exe` with the button above. All versions are on the [Releases page](https://github.com/djshiye/hestia-command/releases).
2. **Open it.** Windows may show *"Windows protected your PC"* because Hestia isn't code-signed. Click **More info**, then **Run anyway**. You only see this once.
3. **Click Install** on the orange banner. That's it. Hestia is added to your Start menu and desktop and opens by itself. No admin rights are needed, and you can delete the downloaded file afterwards.
4. If Windows asks about the **firewall**, tick **Private networks** and click **Allow**. Hestia needs this to hear your switches.

Then follow the three steps in the app: **Test**, **Setup**, **Control**. A full walkthrough with pictures is in [docs/HOW-TO.md](docs/HOW-TO.md).

| | |
| --- | --- |
| **Updates** | Automatic. Once a day Hestia asks GitHub for a new version, checks it against the published checksum, and installs it the next time the window isn't in use (or right away when you click **Restart and update**). Turn it off with the switch at the bottom of the window. |
| **Start with Windows** | Turn it on in the Control tab. Hestia then sits in the tray after you sign in, so keyboard shortcuts and the tray popup always work. |
| **Uninstall** | Windows Settings → Apps → Installed apps → Hestia → Uninstall. It asks whether to keep your saved switches. |
| **Check the download** (optional) | In PowerShell, `Get-FileHash .\Hestia-Setup.exe` must match `SHA256SUMS.txt` on the release page. |

### Wi-Fi or network cable?

| Task | Network cable OK? |
| --- | --- |
| Test and Control | **Yes**, if the PC is on the same local network as the switch (not a guest or separate network). |
| Setup (reading a switch's key) | **No, needs Wi-Fi once**, to join the switch's setup hotspot. |

**Cable-only PC?** Read the key once on any laptop with Wi-Fi. In Setup, click **Show key**. Then on the cable PC, choose **Enter a key by hand** and paste it.

---

## What Hestia does

| Tab | What it does |
| --- | --- |
| **1. Test** | Checks that the switches on this network accept local control. It changes nothing. |
| **2. Setup** | Reads each switch's local key once, straight from the switch, and stores it encrypted for your Windows user. |
| **3. Control** | Unlocks after the test passes. It has an on/off button with live state, Ctrl+Alt keyboard shortcuts, and the option to start with Windows. |
| **Appearance** | Follows the Windows light or dark theme, or pick **Auto / Light / Dark** at the top right. |
| **Tray icon** | Closing the window leaves Hestia running as a flame icon by the clock. Click it for a small popup with an on/off switch per device, right-click it for a menu with the same plus **Open Hestia** and **Quit**. Hover it to see every switch's state. |
| **Activity** | Logs every command, drop-off and reconnection, so you can check the switch stays reliable with the cloud blocked. You can export the log. |

## Security

Hestia is built to be safe to run on a normal home or office PC:

- **Only your network, plus one daily call to GitHub.** Switch control talks only to private, link-local or loopback addresses. The single exception is the update check: once a day Hestia fetches the latest release details from `api.github.com` over HTTPS. Nothing about you or your switches is sent. The switch at the bottom of the window turns it off.
- **Updates come only from this repository.** The updater accepts a release only from this project's GitHub Releases page over HTTPS, and only when the downloaded program matches the release's `SHA256SUMS.txt`. A corrupted or substituted download is refused. Releases are not signed with a separate key, so whoever controls this GitHub repository controls what gets installed; keep the account protected with two-factor authentication.
- **The window is private.** The app page is served on `127.0.0.1` only, behind a random 256-bit token that changes every run, a Host-header check against DNS rebinding, and a strict Content-Security-Policy. Other websites can't reach it.
- **Keys stay encrypted.** Each switch's key is encrypted with Windows DPAPI for your Windows user, so other users on the PC and copies of the settings file can't read it. Keys are never written to the log, and are shown only when you click **Show key**.
- **Nothing from the network is trusted.** Switch announcements are validated before use, and anything from them is cleaned before it reaches the screen or the log.
- **Per-user install, no admin rights.** Hestia only ever touches its own folder, its two shortcuts and its own registry entries. Uninstall removes all of it.
- **Verifiable releases.** Every release is built by GitHub Actions from this repository, with pinned actions, a published SHA-256 checksum and signed build provenance (`gh attestation verify Hestia-Setup.exe -R djshiye/hestia-command`). The CI run on every change includes static analysis and a scan for known vulnerabilities in dependencies.

Two things to know:

- **Windows warns once** because the download isn't code-signed; a signing certificate costs money. The checksum and provenance above are the free way to check a download.
- **The switch's own protocol is the limit.** Anyone already on the same network can see encrypted commands pass by and could replay one (for example, turn the switch on again later). They can't read or forge commands without the key. That's how Sonoff designed the local mode, so keep the switch on a network you trust.

---

## Alternative: build it yourself

You only need this if you want to change the code. Install [Go 1.24+](https://go.dev/dl/), then on any OS:

```sh
git clone https://github.com/djshiye/hestia-command
cd hestia-command
./build.sh 1.2.0      # creates dist/Hestia-Setup.exe and dist/SHA256SUMS.txt
```

### Publishing a release

No tools needed. On GitHub, open **Actions** → **Build and release Hestia** → **Run workflow**, type a version like `1.2.0`, and click **Run workflow**. A few minutes later the new release is on the Releases page. Before publishing, the action vets and tests the code, builds `Hestia-Setup.exe` and records signed build provenance.

Pushing a tag such as `v1.2.0` does the same. If you run the action with the version left empty, it builds a test copy without publishing anything.

### Testing without a switch

`cmd/fakedevice` simulates a Sonoff switch in LAN mode:

```sh
go run ./cmd/fakedevice -iface eth0 &
HESTIA_SETUP_HOST=127.0.0.1:8090 go run -tags dev . -no-window
```

### How the local protocol works

Switches announce themselves over mDNS (`_ewelink._tcp`) with their state in encrypted TXT records. Commands are HTTP POSTs to `http://<switch>:8081/zeroconf/switch`. The payload is AES-128-CBC encrypted with `MD5(device key)` and a fresh random IV. In setup mode the switch serves its device key at `http://10.10.7.1/device`.

## Status

Tested against the simulator and under Wine (install, shortcuts, uninstall, encrypted key storage, start with Windows). Not yet verified on a real Sonoff RFR2.
