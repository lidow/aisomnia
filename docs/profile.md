# Profile reference

The profile is a TOML file. `aisomnia` ships a built-in default; `aisomnia profile` prints the active one, `aisomnia profile --init` writes an editable copy to `~/.config/aisomnia/profile.toml` (or `$XDG_CONFIG_HOME/aisomnia/profile.toml`). Precedence: `--profile PATH`, then `AISOMNIA_PROFILE`, then the user copy, then the built-in default. Unknown keys are rejected so a typo cannot silently do nothing.

## `[pmset.battery]` and `[pmset.ac]`

Integer values passed to `pmset -b` (battery) and `pmset -c` (AC). A key your Mac does not offer (`pmset -g cap` does not list it) is ignored: `status` does not show it and `apply` does not touch it. Example: `lidwake` on Apple silicon MacBooks, which always wake when the lid opens. If the capability list cannot be read, such a key is shown as `UNSUPPORTED` and skipped.

| Key | Default (battery / AC) | Meaning |
|---|---|---|
| `sleep` | 0 / 0 | Minutes of idle before system sleep. 0 disables idle sleep. |
| `displaysleep` | 5 / 30 | Minutes of idle before the display turns off. |
| `disksleep` | 10 / 10 | Minutes before disks spin down (irrelevant for SSDs). |
| `tcpkeepalive` | 1 / 1 | Keep TCP connections alive during sleep. |
| `womp` | 0 / 1 | Wake on LAN. |
| `lidwake` | 1 / 1 | Wake when the lid opens. Intel Macs only; Apple silicon always wakes on lid open, so the key is ignored there. |
| `powernap` | 1 / 1 | Short maintenance wakes during sleep. |
| `standby` | 1 / 1 | Move to standby (RAM off) after a long sleep. |
| `hibernatemode` | 3 / 3 | 3 keeps RAM powered and writes an image; 25 writes the image and powers RAM off. |

## `[power_button]`

| Key | Default | Meaning |
|---|---|---|
| `sleep_on_power_button` | 0 | `Sleep On Power Button` in `/Library/Preferences/com.apple.PowerManagement.plist`. 0 makes a short press turn the display off instead of sleeping. Takes effect after a restart. |
| `loginwindow_sleeps_system` | false | `PowerButtonSleepsSystem` in `/Library/Preferences/com.apple.loginwindow`. Takes effect after logging out and in. |

Remove a key to declare that it must be absent; `apply` then deletes it (this is what the reset baseline does).

## `[screen]`

| Key | Default | Meaning |
|---|---|---|
| `saver_idle_seconds` | 300 | Screen saver idle time, written per host with `defaults -currentHost`. |
| `lock_delay` | `"immediate"` | Lock delay after the screen saver starts or the display turns off: `"immediate"`, `"off"`, or seconds as a string. Set with `sysadminctl -screenLock`, which asks for your login password in the terminal; the password never passes through `aisomnia`. |

## `[lidwatch]`

| Key | Default | Meaning |
|---|---|---|
| `enabled` | true | Run the lid watcher as a launchd user agent. |
| `poll_seconds` | 2 | Clamshell poll interval. |
| `battery_floor_percent` | 20 | A lid hold ends on its own at this battery level. |

## Example: longer lock on AC, no watcher

```toml
name = "my profile"

[pmset.battery]
sleep = 0
displaysleep = 5
tcpkeepalive = 1
womp = 0

[pmset.ac]
sleep = 0
displaysleep = 30
tcpkeepalive = 1
womp = 1

[power_button]
sleep_on_power_button = 0
loginwindow_sleeps_system = false

[screen]
saver_idle_seconds = 600
lock_delay = "immediate"

[lidwatch]
enabled = false
```

Keys you leave out of `[pmset.*]` are simply not managed: `status` does not list them and `apply` does not touch them.
