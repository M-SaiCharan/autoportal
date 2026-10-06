# autoportal

**Stay signed in to your campus Wi-Fi / Ethernet without ever seeing the login page again.**

Many colleges put the network behind a captive portal (the "Sign in to access the network" page).
You get logged out every time your laptop sleeps, restarts or changes network, and have to type your
password again. autoportal sits quietly in your menu bar / system tray and logs you back in within
a second or two, every time.

- **Works on** Windows, macOS (Intel and Apple Silicon) and Linux (Ubuntu and others)
- **Supports** Sophos Firewall (SFOS) and Cyberoam captive portals. Other brands are planned ([adding one](#adding-another-portal-brand))
- **Light:** one ~9 MB file, about 15 MB of RAM, nothing else to install
- **Safe:** your password lives in your OS keychain, and is only ever sent to *your* portal ([details](#security--privacy))

| Icon | Meaning |
|:-:|---|
| 🟢 ✓ | Signed in |
| 🟡 ⋯ | Signing in / checking |
| 🟡 ! | Portal not responding, retrying |
| 🔴 ! | Needs you: wrong password, certificate changed, or extra login step |
| 🔵 ⏸ | Paused (you logged out on purpose) |
| ⚪ – | Not on the campus network |
| ⚪ ✕ | No network |

## Install

### Windows
Open **PowerShell** and paste:
```powershell
irm https://raw.githubusercontent.com/M-SaiCharan/autoportal/main/scripts/install.ps1 | iex
```
Or download `autoportal-windows-amd64.exe` from the [latest release](https://github.com/M-SaiCharan/autoportal/releases/latest) and double-click it.
Windows may warn *"Windows protected your PC"* the first time. Click **More info → Run anyway**. (The app isn't code-signed, because signing certificates cost money. This happens only once.)

### macOS
Open **Terminal** and paste:
```sh
curl -fsSL https://raw.githubusercontent.com/M-SaiCharan/autoportal/main/scripts/install.sh | sh
```
Or download `autoportal-macos.zip` from the [latest release](https://github.com/M-SaiCharan/autoportal/releases/latest), unzip it, and **drag autoportal.app into Applications**.
The first time you open it, macOS will say it can't verify the developer. Open **System Settings → Privacy & Security**, scroll down, and click **Open Anyway**. This happens only once.

### Linux
```sh
curl -fsSL https://raw.githubusercontent.com/M-SaiCharan/autoportal/main/scripts/install.sh | sh
```
Or download `autoportal-linux-amd64`, then `chmod +x autoportal-linux-amd64 && ./autoportal-linux-amd64`.
The tray icon needs a desktop with AppIndicator support. Ubuntu has it built in. On plain GNOME (e.g. Fedora), install the *AppIndicator and KStatusNotifierItem Support* extension.

### First run
1. autoportal looks for your network's login portal. If it can't find it, paste the address of the login page (e.g. `https://10.10.10.2:8090/httpclient.html`).
2. Enter your username and password. autoportal signs in once to check them.
3. Choose **Start automatically**. That's it.

## Using it
You don't have to do anything. Click the icon for:

- **Log in now**: sign in immediately
- **Log out**: sign out *and pause* auto-login, so it doesn't sign you straight back in. **Log in now** resumes it.
- **Pause auto-login**: stop logging in automatically without logging out
- **Change credentials…**: new password, username or portal
- **Start at login**: turn autostart on or off
- **Open log file**: see what happened and when (passwords are never logged)
- **Quit**: you stay signed in, but autoportal stops watching

If your password is rejected, autoportal **stops after one try** and tells you, so it can never lock your account by retrying.

### Dual boot
Windows and Linux on the same laptop are separate installs: set it up once in each.

### Command line
```
autoportal status      # am I signed in?
autoportal login       # sign in once, now
autoportal logout      # sign out once, now
autoportal setup       # set up in the terminal instead of dialogs
autoportal install     # start at login
autoportal uninstall   # stop starting at login (--purge also deletes settings and password)
autoportal run         # no tray icon; logs to the terminal (servers, debugging)
```
On Windows, use `autoportal-cli-windows-amd64.exe` for terminal use. The normal `.exe` is a windowless app.

## Tips

### Stop the macOS "Log in to network" popup
macOS opens its own mini-browser when it detects a captive portal. With Sophos it often shows a blank
page, and with autoportal you don't need it. To turn it off:
```sh
sudo defaults write /Library/Preferences/SystemConfiguration/com.apple.captive.control Active -bool false
```
Then turn Wi-Fi off and on. This affects **all** networks: at hotels or airports, open any `http://` site
(e.g. <http://neverssl.com>) to reach their login page. To undo:
```sh
sudo defaults delete /Library/Preferences/SystemConfiguration/com.apple.captive.control Active
```

### Ubuntu "Hotspot login required" notification
Settings → Privacy → **Connectivity Checking** → off.

## How it works
1. Every 3 seconds autoportal checks, **locally and without network traffic**, whether the computer just
   woke up (the clock jumped) or the network changed (interface addresses changed).
2. If so, it checks for the next minute whether the internet works, using the same check URLs that
   Android, Windows and macOS use themselves (`generate_204` and friends), and whether your portal is reachable.
3. Portal reachable and internet blocked: it sends one login request, exactly like the portal's own web page does.
   It also logs in once after every wake, even if the internet looks fine, in case your network allows those check URLs through.
4. Otherwise it re-checks once a minute.

See [docs/PROTOCOL.md](docs/PROTOCOL.md) for the Sophos protocol details.

## Security & privacy
- **Password:** stored only in the OS keychain (macOS Keychain, Windows Credential Manager, GNOME Keyring / KWallet).
  The settings file (`config.json`) holds only the portal address, username and certificate fingerprint.
- **Certificate pinning:** at setup autoportal records the portal's HTTPS certificate fingerprint and
  afterwards refuses to send your password to anything else. A fake hotspot pretending to be your portal gets nothing.
  If your college renews its certificate, you'll get a 🔴 icon and a notification asking you to run setup again.
- **Network traffic:** only your portal (on the local network) and the standard connectivity-check URLs.
  No analytics, no telemetry, no other servers.
- **No admin rights:** everything installs into your user folders.

## Building from source
Requires Go (see `go.mod`).
```sh
make test     # run tests (they use a built-in fake portal, no campus network needed)
make build    # binary for this machine → dist/autoportal
make dist     # Linux + Windows binaries (cross-compiled)
make macos    # macOS .app (run on a Mac: the menu bar needs Cocoa via cgo)
```
Releases are built by GitHub Actions when a `v*` tag is pushed (`.github/workflows/release.yml`).

### Adding another portal brand
Portal brands are drivers under `internal/portal/`. Implement `portal.Adapter` (`Reachable`, `Login`,
`Logout`) and a `Detect` function, then register them with `portal.Register` in an `init()`. Look at
`internal/portal/sophos` as a template, and add a fake server under `.../<brand>test` for tests.
Contributions for FortiGate, pfSense/OPNsense, Aruba and others are welcome.

## Roadmap
- v2: data-usage display (from the Sophos user portal), with a choice of what the menu shows
- More portal brands (FortiGate, pfSense/OPNsense, generic HTML forms)
- In-app toggle for the macOS captive popup

## Disclaimer
autoportal is an independent project, not affiliated with or endorsed by Sophos. It does exactly what
the portal's own login page does, using your own credentials. Use it in line with your institution's
network policies.

## License
[MIT](LICENSE)
