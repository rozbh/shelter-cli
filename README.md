# Shelter CLI

> An unofficial command-line client for [ShelterTM](https://www.sheltertm.com/) that makes connecting and managing Shelter DNS from your terminal simple and fast.

> **Disclaimer**
>
> This project is **not affiliated with, endorsed by, or maintained by ShelterTM**. It is a community-built client intended to make using the Shelter platform easier from the command line.

## Features

* 🚀 Simple terminal interface powered by Bubble Tea
* 🌐 Connect to Shelter without using the web interface
* 🔄 Automatic connectivity monitoring
* 📡 Register your current IP with your Shelter account
* ⚙️ Automatic system DNS configuration
* 💾 Persistent configuration
* 📝 Timestamped logging
* 🖥️ Cross-platform support (Linux, macOS, and Windows)

---

## Project Structure

```text
cmd/ipbox/
    Application entry point
    • Signal & panic handling
    • DNS reset on exit
    • Starts the TUI

internal/tui/
    Bubble Tea interface
    • Views
    • Connectivity monitoring
    • Status updates

internal/shelter/
    Shelter integration
    • Session handling
    • Register current IP
    • Connection workflow

internal/dns/
    Platform-specific DNS management
    • Linux (resolvectl)
    • macOS (networksetup)
    • Windows (netsh)
    • DNS verification

internal/config/
    Configuration loading and saving

internal/logging/
    Shared timestamped logger
```

---

# Installation

Clone the repository:

```bash
git clone https://github.com/rozbh/shelter-cli.git
cd shelter-cli
```

Download dependencies:

```bash
go mod tidy
```

Build:

```bash
go build -o bin/ipbox ./cmd/ipbox
```

---

# Running

### Linux / macOS

Administrator privileges are required because the application modifies your system DNS settings.

```bash
sudo ./bin/ipbox
```

### Windows

Run the executable from an elevated Command Prompt or PowerShell.

---

# Development

You can also run directly with Go:

```bash
go run ./cmd/ipbox
```

Configuration and log files are created in the current working directory.

---

# Configuration

The application stores its configuration in:

```text
shelter_config.json
```

Logs are written to:

```text
shelter.log
```

---

# Linux Notes

On Linux, DNS is managed using **systemd-resolved** through `resolvectl`.

Because `/etc/resolv.conf` is typically a symbolic link managed by systemd-resolved, editing it directly will not work.

Running the application with `sudo` is therefore required. Without elevated privileges, DNS configuration will fail and Shelter connections cannot be established.

---

# Keyboard Shortcuts

| Key      | Action                      |
| -------- | --------------------------- |
| `r`      | Refresh connectivity status |
| `c`      | Reconfigure the application |
| `q`      | Quit                        |
| `Esc`    | Quit                        |
| `Ctrl+C` | Quit                        |

When exiting, the application automatically restores your DNS servers to:

* `8.8.8.8`
* `1.1.1.1`

---

# How It Works

The application streamlines the Shelter connection process by:

1. Authenticating with Shelter.
2. Registering your current public IP.
3. Configuring your system DNS.
4. Monitoring connectivity in real time.
5. Restoring DNS settings when the application exits.

This makes connecting to Shelter significantly faster than repeatedly using the web interface while also making the workflow scriptable and terminal-friendly.

---

# Logging

A timestamped log is written to:

```text
shelter.log
```

This is useful for troubleshooting DNS configuration, connectivity issues, and Shelter communication.

---

# Disclaimer

This project is an independent open-source client.

It is **not** an official [ShelterTM](https://www.sheltertm.com/) product and is not affiliated with or endorsed by ShelterTM. All trademarks and service marks belong to their respective owners.

---

# License

This project is released under the license included in this repository.
