# Hestia

**Control your Sonoff switches from your PC, over your own network. No cloud, no eWeLink account, no changes to the switch.**

![Hestia control screen](docs/screenshot-control.png)

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
| **Update** | Download the new `Hestia-Setup.exe`, open it and click **Update**. Your switches and keys are kept. |
| **Start with Windows** | Turn it on in the Control tab. Hestia then runs quietly after you sign in, so keyboard shortcuts always work. |
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
| **3. Control** | Unlocks after the test passes. It has an on/off button with live state, Ctrl+Alt keyboard shortcuts, and options to run in the background and start with Windows. |
| **Activity** | Logs every command, drop-off and reconnection, so you can check the switch stays reliable with the cloud blocked. You can export the log. |

## Security

- The app window is served on `127.0.0.1` only. It's protected by a random per-run token, a Host-header check against DNS rebinding and a strict Content-Security-Policy.
- Keys are encrypted with Windows DPAPI for the current user. They are never written to the log, and are shown only when you click **Show key**.
- Hestia refuses to contact anything outside private, link-local or loopback addresses.
- It installs per user, with no admin rights, and touches only its own folder, shortcuts and registry entries.
- Releases are built by GitHub Actions from this repository, with signed build provenance. To check one: `gh attestation verify Hestia-Setup.exe -R djshiye/hestia-command`.

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
