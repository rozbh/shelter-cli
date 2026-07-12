# shelter-cli

Unofficial terminal client for [sheltertm.com](https://www.sheltertm.com/) — network connectivity monitor + DNS manager.

Not affiliated with or endorsed by Shelter. Built to automate connecting through the Shelter panel and keeping your DNS pointed at their servers from the terminal.

## What it does

- **Connectivity table** — public IP, ping to 8.8.8.8 / 1.1.1.1, DNS resolution checks. Auto-refreshes every 10s.
- **Auto-connect to Shelter** — on open, and on every refresh while you have internet but aren't connected, it logs into the Shelter panel and registers your IP automatically. No manual login step.
- **System DNS management** — once Shelter confirms the connection, it points your system DNS at the servers you configured (Linux via `resolvectl`, macOS via `networksetup`, Windows via `netsh`).
- **DNS verification** — after setting DNS, it actually queries through the new servers to confirm they resolve, not just that the command didn't error.
- **Safety net** — on crash, `Ctrl+C`, `kill`, or normal quit, it resets your DNS back to `8.8.8.8` / `1.1.1.1` so you're never stuck on a dead resolver.
- **Logging** — every step (panel requests, cookies, DNS commands, verification) is written to `shelter.log` in the working directory for debugging.

## Requirements

- Go installed (for `go run .`) or a compiled binary
- **root/admin required** — setting system DNS needs elevated privileges. On Linux this goes through `resolvectl`/polkit, so run the whole app with `sudo`, not just individual commands.

## Usage

```bash
# quick run
sudo go run .

# or build once, run many times
go build -o shelter-cli .
sudo ./shelter-cli
```

On first run you'll be asked for:
- `DNS 1` / `DNS 2` — the DNS servers to switch to once connected
- `DNS Key` — your Shelter access key (used to build the panel URL and register your IP)

These are saved to `shelter_config.json` in the working directory.

### Keys (main screen)

| Key | Action |
|---|---|
| `r` | refresh checks now |
| `c` | reconfigure (re-enter DNS1/DNS2/key) |
| `q` / `Esc` / `Ctrl+C` | quit (resets DNS to fallback) |

## Debugging

```bash
tail -f shelter.log
```
Shows exactly what's happening at each stage — panel fetch, cookies, register-ip response, DNS command run, verification query.

## Disclaimer

Unofficial, community-built tool. Not created by, maintained by, or affiliated with Shelter. Use at your own risk.