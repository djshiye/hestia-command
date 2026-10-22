# Hestia 1.0.0: how to test it at the office

Hestia is one Windows program. It runs only on this PC and talks only to devices on the local network. It never contacts eWeLink or any internet service.

**File check (optional):** the SHA-256 fingerprint of `Hestia.exe` is
`d86de4298bcf6e5f12edb63384c6710ac8596f78e16d9687484759a5d29ed566`.
In PowerShell: `Get-FileHash .\Hestia.exe`

## Before you start
- Use a Windows 10 or 11 PC on the **same Wi-Fi as the switch**. A guest network won't work.
- Leave the switch as it is today (working in the eWeLink app).

## 1. Open Hestia
- Double-click `Hestia.exe`. No installer is needed.
- Windows will say "Windows protected your PC" because the program isn't code-signed. Click **More info**, then **Run anyway**.
- If Windows asks about the firewall, tick **Private networks** and click **Allow**. Hestia needs this to hear the switch.

## 2. Test tab
- Click **Run test**. It takes about 8 seconds.
- **Passed**: the switch supports local control. Go to Setup.
- **Not passed**: take a screenshot of the checks and send it to me.

## 3. Setup tab
1. Hold the switch's button for about 5 seconds until the blue light blinks fast. Then hold it again for about 5 seconds until the blinking changes.
2. On the PC, join the Wi-Fi named `ITEAD-10000…` with password `12345678`.
3. Click **Read key from switch**. It says "Key saved" within a few seconds.
4. Reconnect the PC to the office Wi-Fi. If the switch doesn't come back by itself within a few minutes, add it again in the eWeLink app as usual.

The key is stored encrypted for your Windows user. You never see it, and it never leaves the PC.

## 4. Control tab
- This tab unlocks once the test has passed and the key is confirmed.
- Click the big round button to turn the switch on or off.
- Under **Shortcut**, pick a key combination such as Ctrl+Alt+1. Then turn on **Keep running after closing this window** so the shortcut works with the window closed.
- Use **Quit Hestia** to stop it completely.

## 5. Activity tab
- This tab shows every on/off command, drop-off and reconnection.
- After the cloud is blocked, leave Hestia open for 48 hours. Then click **Export log** and send me the file.

## Where Hestia keeps its files
`%AppData%\Hestia` holds the settings, the encrypted key and a log. The log contains no keys. Deleting the folder resets Hestia.
