# shelter-cli (ipbox)

Terminal connectivity monitor + shelter DNS manager.

## Layout

```
cmd/ipbox/          entrypoint: signal/panic handling, DNS-reset-on-exit, starts the TUI
internal/tui/       bubbletea model, views, connectivity checks
internal/shelter/   panel session fetch + register-ip + connect flow
internal/dns/       per-OS system DNS set (resolvectl/networksetup/netsh) + verification
internal/config/    load/save of shelter_config.json
internal/logging/   shared timestamped logger (stderr + shelter.log)
```

## Run

```bash
go mod tidy
go build -o bin/ipbox ./cmd/ipbox
sudo ./bin/ipbox        # linux/macOS: sudo required, resolvectl/networksetup need root
```

On Linux, DNS is managed via `resolvectl` (systemd-resolved) — `/etc/resolv.conf` is a
symlink there, so writing it directly does nothing. `sudo` is required or every connect
attempt fails at the set-DNS step.

For development, `go run ./cmd/ipbox` also works — config is written to the current
working directory either way, so run it from wherever you want `shelter_config.json`
and `shelter.log` to live.

## Keys

- `r` — refresh connectivity now
- `c` — reconfigure (back to setup screen)
- `q` / `esc` / `ctrl+c` — quit (resets DNS to 8.8.8.8/1.1.1.1 on the way out)
