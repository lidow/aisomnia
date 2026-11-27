# States and transitions

macOS has five states that matter here. "Display off" and "locked" are independent: a dark screen can be unlocked and a lock screen can be lit. The network is down in normal sleep except during short Power Nap wakes.

```mermaid
stateDiagram-v2
    direction LR
    [*] --> Active
    Active --> DisplayOff: display timer (AC 30 min, battery 5 min)\nor power button\nor aisomnia lock
    Active --> Locked: Control-Command-Q, Lock Screen,\nscreen saver 5 min + immediate lock
    DisplayOff --> Locked: lock delay "immediate"
    Locked --> DisplayOff: display timer while locked
    Locked --> Active: password or Touch ID
    DisplayOff --> Active: any key, if not yet locked
    Active --> Sleep: lid close, aisomnia sleep, Apple menu > Sleep
    Locked --> Sleep: lid close
    DisplayOff --> Sleep: lid close
    Sleep --> Locked: lid open or key
    Sleep --> Standby: after the standby delay
    Standby --> Locked: wake (resume from the sleep image)
    Sleep --> DarkWake: Power Nap, TCP keep-alive
    DarkWake --> Sleep
```

| State | Display | Locked | CPU and work | Network | How you get there | How you leave |
|---|---|---|---|---|---|---|
| Active | on | no | runs | up | wake and authenticate | timers, keys, lid |
| Display off | off | not until the lock delay | runs | up | display timer, power button, `aisomnia lock` | any key |
| Locked | on or off | yes | runs | up | screen saver + immediate lock, Control-Command-Q, display off + immediate lock | password or Touch ID |
| Sleep | off | yes on wake | halted | down (dark wakes excepted) | lid close only, once the profile is applied | lid open, key, power button |
| Standby (deep sleep) | off | yes on wake | halted, RAM off | down | macOS after the standby delay | power button, lid open |

## Force and hold

| State | Happens when | Force it now | Hold it off once |
|---|---|---|---|
| Active | Any wake | `aisomnia wake-at "2026-01-15 07:30"`, `aisomnia wake-in 2h` | not applicable |
| Display off | Display timer; power button; `aisomnia lock` | `aisomnia display-off` | `aisomnia hold display 2h` |
| Locked | Screen saver after 5 min; display off; Control-Command-Q; Touch ID press | `aisomnia lock` | `aisomnia hold lock 1h` (only while you are at the Mac) |
| Sleep | Lid close (idle sleep is off, the button no longer sleeps) | `aisomnia sleep` | `aisomnia hold lid 2h` (sudo once; ends at 20 % battery) |
| Standby | During sleep, after macOS's standby delay | `aisomnia hibernate` | `aisomnia hold standby 8h` (sudo once) |
| Dark wake | During sleep: Power Nap, TCP keep-alive | not forceable | set `powernap = 0` in the profile |
