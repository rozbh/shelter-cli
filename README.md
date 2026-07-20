# shelter-cli

**A cross-platform terminal connectivity monitor and DNS manager for [Shelter](https://sheltertm.com) — written in Go.**

shelter-cli watches your internet connection in a live terminal UI, registers your public IP with the Shelter panel, applies custom DNS system-wide, and verifies that resolution is actually working — all without leaving the terminal.

---

## Features

- **Live connectivity dashboard** — pings 8.8.8.8, 1.1.1.1, google.com, and a local target, auto-refreshing every 10 seconds
- **Public IP detection** — fetches your current public IP independent of system DNS
- **Shelter panel authentication** — registers your IP against the Shelter panel automatically, with panel3 → panel2 fallback
- **System-wide DNS management** — applies custom DNS servers on Linux (NetworkManager / resolvectl), macOS (networksetup), and Windows (PowerShell `Set-DnsClientServerAddress`) — always binding to the actual active network interface, not just the first one found
- **DNS verification** — confirms your new DNS settings actually resolve before reporting success
- **Safe by default** — automatically resets DNS to public fallbacks (8.8.8.8 / 1.1.1.1) on crash, Ctrl+C, or normal exit
- **Clean TUI** — built with [Bubbletea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss)

## Why shelter-cli?

Shelter's official client requires a GUI. shelter-cli is an unofficial, open-source, terminal-first alternative for people who live in the terminal, run headless servers, or want a lightweight always-on connectivity + DNS watchdog.

## Install

### From source

```bash
git clone https://github.com/YOUR_USERNAME/shelter-cli.git
cd shelter-cli
go build -o shelter-cli ./cmd/shelter
sudo ./shelter-cli
```

### Prebuilt binaries

Grab the latest release for Linux (amd64/arm64) or Windows (amd64) from the [Releases page](https://github.com/YOUR_USERNAME/shelter-cli/releases). Nightly builds are published automatically from `main`.

> **Note:** shelter-cli must run with elevated privileges (sudo / Administrator) because setting system DNS requires root access.

## Usage

```bash
sudo shelter-cli
```

On first run you'll be asked for:

| Field    | Description                     |
|----------|----------------------------------|
| `dns1`   | Primary custom DNS server (IP)   |
| `dns2`   | Secondary custom DNS server (IP) |
| `dnskey` | Your Shelter panel DNS key       |

Once configured, shelter-cli will:
1. Check internet connectivity
2. Fetch your public IP
3. Register it with the Shelter panel
4. Apply `dns1`/`dns2` system-wide
5. Verify DNS resolution
6. Keep monitoring and auto-reconnect if anything drops

**Keybinds:** `r` refresh now · `c` reconfigure · `q` / `Esc` quit

## How it works

- **Linux** — finds the active interface via `ip route get`, then sets DNS on the bound NetworkManager connection profile (`nmcli`), falling back to `resolvectl`
- **macOS** — maps the default route's BSD device to its NetworkManager service name via `networksetup`
- **Windows** — resolves the default route's interface index via `Get-NetRoute`, then applies DNS with `Set-DnsClientServerAddress`

All platforms bind to the real active interface/connection object rather than guessing by name, so DNS changes stick and survive DHCP renewal.

## Building / Contributing

```bash
go build ./...
go test ./...
```

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Please open an issue before large changes.

## License

GPLv3 — see [LICENSE](LICENSE).

---

*Keywords: Shelter DNS client, Shelter panel CLI, sheltertm terminal client, Go DNS manager, cross-platform DNS CLI, network connectivity monitor TUI, unofficial Shelter client.*