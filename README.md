# sizewatch

Tracks folder growth and runs a hook on spikes.

Point it at the folders that tend to balloon (Downloads, caches,
`node_modules`, build outputs, log directories) and it measures them on an
interval, keeps a small history, and runs your command when one of them
grows too much or too fast. It walks trees concurrently, counts real disk
usage the way `du` does, and ships as a single static binary.

## Install

```sh
go install github.com/dan2756/sizewatch@latest
```

## Quick start

```sh
# Watch two folders, check every 5 minutes, tell me about +500 MB jumps.
sizewatch watch ~/Downloads ./node_modules --interval 5m --spike 500MB \
  --hook 'notify "$SIZEWATCH_PATH grew by $SIZEWATCH_DELTA_HUMAN"'

# What has been happening?
sizewatch report

# Just the size, please.
sizewatch du ~/Library/Caches
```

## Commands

### `sizewatch watch PATH... [flags]`

Measures every path each `--interval` (default `5m`), appends the samples to
the history and evaluates the triggers against the previous sample of each
path. Runs until Ctrl-C or SIGTERM; stopping mid-round still records the
paths measured so far and their alerts.

```
$ sizewatch watch ~/Downloads ~/proj/node_modules --spike 500MB --max 20GB
sizewatch: watching 2 paths every 5m, history in /Users/dj/.local/state/sizewatch/history.jsonl
2026-08-15 19:05:00  /Users/dj/Downloads          11.8 GB   +0 B in 5m
2026-08-15 19:05:00  /Users/dj/proj/node_modules  412.0 MB  +0 B in 5m
2026-08-15 19:10:00  /Users/dj/Downloads          12.4 GB   +600.0 MB in 5m  ALERT spike
2026-08-15 19:10:00  /Users/dj/proj/node_modules  412.0 MB  +0 B in 5m
2026-08-15 19:15:00  /Users/dj/Downloads          13.1 GB   +700.0 MB in 5m  spike (cooldown)
```

A watch picks up where the history left off, so its first round already
compares against the last sample of an earlier run.

### `sizewatch once PATH... [flags]`

Takes one sample of each path and evaluates the triggers against the last
stored sample. It takes the same trigger, hook and history flags as `watch`,
which makes it the thing to run from cron or launchd (see below).

```
$ sizewatch once ~/Downloads ~/proj/node_modules --spike 500MB
/Users/dj/Downloads          12.4 GB   +600.0 MB in 1h  ALERT spike
/Users/dj/proj/node_modules  412.0 MB  first sample
```

`--quiet` prints only paths that alerted, so cron stays silent otherwise.

### `sizewatch report [PATH...] [flags]`

Shows what the history knows: the latest size and file count, the change
over the last hour, day and week, how long ago the last sample was taken and
a sparkline of the most recent samples (`--points`, default 24). Without
`PATH` it lists every path in the history.

```
$ sizewatch report
PATH                         SIZE      FILES  1H        24H        7D        UPDATED  TREND
/Users/dj/Downloads          11.8 GB   3191   +55.0 MB  +360.0 MB  +2.5 GB   4m ago   ▁▁▂▂▂▃▃▃▃▄▄▄▅▅▅▆▆▆▆▇▇▇██
/Users/dj/proj/node_modules  490.0 MB  40000  +0 B      +0 B       +90.0 MB  4m ago   ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁
```

Changes are measured back from the latest sample, against the newest sample
at least that old. A column shows `-` when the history doesn't reach back
far enough, or when the nearest sample is more than half a window older than
that (a day-old sample isn't passed off as the 1h change).

### `sizewatch du PATH... [flags]`

Prints the size of each path and records nothing.

```
$ sizewatch du ~/Downloads ./node_modules
  12.4 GB  /Users/dj/Downloads
 412.0 MB  ./node_modules
```

### JSON

`once`, `report` and `du` take `--json`:

```sh
sizewatch du --json ~/Downloads | jq '.[0].bytes'
sizewatch report --json | jq -r '.[] | "\(.path) \(.change["24h"])"'
```

- `du`: `[{path, bytes, files, dirs, warnings, error?}]`
- `once`: `[{time, path, bytes, files, prev_time, prev_bytes, delta_bytes,
  warnings, reasons, alert?, hook_error?, error?}]`; `prev_*` are `null` on
  the first sample and `alert` is `fired` or `cooldown`
- `report`: `[{path, time, bytes, files, change: {"1h", "24h", "7d"},
  samples: [{time, bytes}]}]`, with `null` for unknown changes

With `--json`, a hook's standard output goes to stderr so that stdout stays
valid JSON.

## Flags

Measurement (`watch`, `once`, `du`):

| Flag | Meaning |
| --- | --- |
| `--apparent` | sum file sizes instead of allocated disk usage |
| `--exclude GLOB` | skip matching entries; repeatable (see below) |
| `--one-file-system` | don't descend into other mounted file systems, like `du -x` |
| `--workers N` | concurrent directory walkers (default 2 per CPU, between 4 and 32) |

Triggers and hooks (`watch`, `once`):

| Flag | Meaning |
| --- | --- |
| `--spike SIZE` | growth since the previous sample of at least SIZE |
| `--spike-pct N` | growth since the previous sample of at least N percent |
| `--rate SIZE/PERIOD` | growth faster than this between two samples, e.g. `1GB/h` |
| `--max SIZE` | the folder is at least SIZE |
| `--hook COMMAND` | run COMMAND with `sh -c` when an alert fires |
| `--cooldown DURATION` | after an alert, hold back further alerts for that path (default `30m`, `0` disables) |
| `--quiet` | print only alerts and problems |

History:

| Flag | Meaning |
| --- | --- |
| `--history FILE` | history file to use, overriding `SIZEWATCH_HISTORY` (`watch`, `once`, `report`) |
| `--keep DURATION` | drop samples older than this (default `30d`, `0` for no limit; samples older than a day are thinned to hourly either way) (`watch`, `once`) |

`watch` also takes `--interval DURATION` (default `5m`); `report` takes
`--points N`. Flags may come before or after the paths, and `--` ends the
flags. Every command has `-h`, and `sizewatch --version` prints the version.

## Triggers

Any combination of triggers can be set; a sample alerts when at least one of
them fires.

- `spike`, `pct` and `rate` compare with the previous sample of the same
  path, so they never fire on the first one, and shrinking never fires them.
  `pct` also needs a non-empty previous sample, one with at least one file
  in it: growth from an empty folder isn't a meaningful percentage, whether
  the folder took 0 bytes (APFS) or a 4 KB directory block (most Linux file
  systems).
- `rate` divides the growth by the actual time between the two samples, so
  `--rate 1GB/h` means the same with `watch --interval 1m` as with an
  hourly cron job.
- `max` fires whenever the folder is at or above the ceiling, including on
  its first sample. Pair it with a long `--cooldown` if a folder is meant to
  stay large.

After a path alerts it is held back for `--cooldown`: the table then shows
`spike (cooldown)` instead of `ALERT spike` and the hook doesn't run. The
cooldown is saved next to the history (`history.cooldown.json`), so it
holds across separate `once` runs and `watch` restarts.

## Hooks

The hook runs with `sh -c`, inherits sizewatch's environment and gets these
variables on top:

| Variable | Example | Meaning |
| --- | --- | --- |
| `SIZEWATCH_PATH` | `/Users/dj/Downloads` | absolute path of the folder |
| `SIZEWATCH_BYTES` | `12400000000` | current size in bytes |
| `SIZEWATCH_BYTES_HUMAN` | `12.4 GB` | current size, formatted |
| `SIZEWATCH_PREV_BYTES` | `11800000000` | previous size; empty on the first sample |
| `SIZEWATCH_DELTA_BYTES` | `600000000` | growth since the previous sample (0 on the first) |
| `SIZEWATCH_DELTA_HUMAN` | `+600.0 MB` | growth, formatted with a sign |
| `SIZEWATCH_REASON` | `spike` | first trigger that fired: `spike`, `pct`, `rate` or `max` |
| `SIZEWATCH_REASONS` | `spike,pct` | every trigger that fired, comma-separated |

The hook runs once per alerting path per sample, before the sample is
written to the history (so a full disk still gets its alert). It runs in the
foreground and `watch` waits for it before measuring the next path, so put
anything slow in the background with `&`. A failing hook is reported on
stderr and makes `once` exit 1. Stopping `watch` kills a running hook along
with the processes it started.

Examples:

```sh
# macOS notification
--hook 'osascript -e "display notification \"grew by $SIZEWATCH_DELTA_HUMAN\" with title \"$SIZEWATCH_PATH\""'

# Linux desktop notification
--hook 'notify-send "$SIZEWATCH_PATH" "grew by $SIZEWATCH_DELTA_HUMAN (now $SIZEWATCH_BYTES_HUMAN)"'

# Append to a log
--hook 'echo "$(date -u +%FT%TZ) $SIZEWATCH_REASON $SIZEWATCH_PATH $SIZEWATCH_BYTES" >> ~/sizewatch-alerts.log'
```

## Sizes, rates and durations

Sizes are a number with an optional unit, case-insensitive, with or without
a space: `4096`, `500MB`, `1.5 GiB`, `10g`.

| Units | Base |
| --- | --- |
| `B` | bytes |
| `KB`, `MB`, `GB`, `TB`, `PB` (or `K`, `M`, `G`, `T`, `P`) | decimal: powers of 1000 |
| `KiB`, `MiB`, `GiB`, `TiB`, `PiB` | binary: powers of 1024 |

Output always uses decimal units, e.g. `1.5 GB` for 1,500,000,000 bytes.
Note that `--spike 1GB` and `--spike 1GiB` differ by about 7%.

Rates are `SIZE/PERIOD`. The period is a unit (`s`, `min`, `h`, `day`,
`week`, or the short and long forms `sec`, `m`, `hr`, `hour`, `d`, `w`) or a
duration: `1GB/h`, `500MB/day`, `10MiB/min`, `2GB/30m`.

Durations use Go's syntax (`90s`, `5m`, `1h30m`) plus days and weeks:
`30d`, `1w`, `1d12h`.

## What gets counted

- **Allocated disk usage** by default: `st_blocks x 512`, like `du`. Sparse
  files count only what is allocated and small files count whole blocks.
  `--apparent` sums the sizes of regular files and symlinks instead, like
  `du --apparent-size` (directories then count nothing).
- **Hard links** are counted once per measured path, by device and inode.
- **Symlinks** are never followed; the link itself is counted. A symlink
  given as `PATH` on the command line is resolved, so `~/Downloads` may be a
  link to another disk.
- **Unreadable entries** (permission denied, deleted mid-walk) are skipped
  and reported as a warning count on stderr and in `--json`. Only an
  unreadable `PATH` itself is an error.
- **`--exclude GLOB`** takes shell-style patterns (`*`, `?`, `[a-z]`). A
  pattern without a slash matches entry names at any depth
  (`--exclude '*.tmp' --exclude .git`); a pattern with a slash matches the
  path relative to the measured folder (`--exclude build/cache`,
  `--exclude '*/node_modules'`). Excluded directories are not entered.
- **`--one-file-system`** skips directories on a different device than the
  measured folder, such as a disk image mounted inside it.

## History

Every sample is one JSON line:

```json
{"time":"2026-08-15T19:10:00Z","path":"/Users/dj/Downloads","bytes":12400000000,"files":3191}
```

The file is the first of:

1. `--history FILE`
2. `$SIZEWATCH_HISTORY`
3. `$XDG_STATE_HOME/sizewatch/history.jsonl`
4. `~/.local/state/sizewatch/history.jsonl` (on macOS too)

`watch` and `once` keep it bounded. Samples older than `--keep` (default
`30d`, `0` for no limit) are dropped, and samples older than a day are
thinned to the last one per path per hour whatever `--keep` is, which is all
the 24h and 7d figures need. The file is only rewritten once at least 64
samples can go. Writers of the history and of the cooldown file take an
advisory lock on `history.jsonl.lock`, so a `watch` and a cron job can share
one history.

Paths are stored absolute. JSON can't hold bytes that aren't valid UTF-8,
so such bytes in a path are stored (and shown) as U+FFFD; hooks still get
the real path. Mixing `--apparent` and allocated samples of the same path
makes its deltas meaningless, so pick one per path.

## Running from cron or launchd

`once` is meant for schedulers. With cron:

```cron
*/10 * * * * $HOME/go/bin/sizewatch once --quiet --spike 1GB --rate 5GB/h "$HOME/Downloads" "$HOME/Library/Caches"
```

With launchd, save this as `~/Library/LaunchAgents/dev.sizewatch.plist` and
run `launchctl load ~/Library/LaunchAgents/dev.sizewatch.plist`. launchd
doesn't expand `~` or `$HOME`, so spell paths out:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.sizewatch</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/dj/go/bin/sizewatch</string>
    <string>once</string>
    <string>--quiet</string>
    <string>--spike</string>
    <string>1GB</string>
    <string>/Users/dj/Downloads</string>
    <string>--hook</string>
    <string>osascript -e "display notification \"$SIZEWATCH_PATH grew by $SIZEWATCH_DELTA_HUMAN\""</string>
  </array>
  <key>StartInterval</key>
  <integer>600</integer>
  <key>StandardErrorPath</key>
  <string>/tmp/sizewatch.err</string>
</dict>
</plist>
```

Hooks started by cron or launchd get a minimal `PATH`; use absolute paths
for anything outside `/usr/bin` and `/bin`.

For a long-running `watch` on Linux, a systemd user unit works:

```ini
# ~/.config/systemd/user/sizewatch.service
[Service]
ExecStart=%h/go/bin/sizewatch watch --quiet --spike 1GB %h/Downloads %h/.cache
Restart=on-failure

[Install]
WantedBy=default.target
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success; alerts may have fired (`watch` exits 0 on Ctrl-C or SIGTERM) |
| 1 | a path couldn't be measured, a hook failed, or the history couldn't be read or written |
| 2 | bad command line |

`watch` reports measurement, hook and history errors on stderr and keeps
going.

## Platform notes

- **Linux and macOS** are fully supported: allocated sizes come from
  `st_blocks`, hard links are matched by `st_dev`/`st_ino`, and
  `--one-file-system` compares `st_dev`.
- **macOS**: APFS clones (Finder's Duplicate, `cp -c`) share blocks on disk
  but each reports its full allocation, so they are counted per copy, as
  `du` counts them. Some folders under `~/Library` are unreadable without
  Full Disk Access for your terminal; their entries show up in the warning
  count.
- **Other platforms** build, but fall back to apparent sizes without hard
  link detection or `--one-file-system`, the history isn't locked, and
  stopping `watch` kills only a running hook's shell, not what it started.

## License

MIT
