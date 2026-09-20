# sizewatch

Tracks folder growth and runs a hook on spikes.

Point it at the folders that tend to balloon (Downloads, caches,
`node_modules`, build outputs, log directories) and it measures them on an
interval, keeps a history, and runs your command when one grows too fast.

## Install

```sh
go install github.com/dan2756/sizewatch@latest
```

## Usage

```sh
sizewatch watch ~/Downloads ./node_modules --interval 5m --spike 500MB \
  --hook 'notify "$SIZEWATCH_PATH grew by $SIZEWATCH_DELTA_HUMAN"'
```

`watch` samples every `--interval` (default 5m) until interrupted. After an
alert, a path stays quiet for `--cooldown` (default 30m) so that a folder
that keeps growing doesn't spam you.

`sizewatch du` just prints sizes:

```sh
sizewatch du ~/Downloads ./node_modules
```

```
  12.4 GB  /Users/dj/Downloads
 412.0 MB  ./node_modules
```

`sizewatch once` takes a single sample, for cron or launchd:

```sh
sizewatch once ~/Downloads ./node_modules
```

```
/Users/dj/Downloads          12.4 GB   +600.0 MB in 5m
/Users/dj/proj/node_modules  412.0 MB  first sample
```

`sizewatch report` shows what the history knows: the latest size, the change
over the last hour, day and week, and a sparkline of recent samples.

```
PATH                         SIZE      FILES  1H        24H        7D        UPDATED  TREND
/Users/dj/Downloads          11.8 GB   3191   +55.0 MB  +360.0 MB  +2.5 GB   4m ago   ▁▁▂▂▂▃▃▃▃▄▄▄▅▅▅▆▆▆▆▇▇▇██
/Users/dj/proj/node_modules  490.0 MB  40000  +0 B      +0 B       +90.0 MB  4m ago   ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁
```

`report`, `once` and `du` accept `--json` for scripts.

Both `watch` and `once` take triggers and a hook. `--spike SIZE` fires on
growth since the previous sample, `--spike-pct N` on percentage growth,
`--rate SIZE/PERIOD` (e.g. `1GB/h`) on growth speed between samples and
`--max SIZE` when a folder reaches a ceiling. The hook runs with `sh -c` and
gets `SIZEWATCH_PATH`, `SIZEWATCH_BYTES`, `SIZEWATCH_PREV_BYTES`,
`SIZEWATCH_DELTA_BYTES`, `SIZEWATCH_DELTA_HUMAN` and `SIZEWATCH_REASON`
(`spike`, `pct`, `rate` or `max`) in its environment.

Sizes accept decimal units (KB, MB, GB, TB = powers of 1000) and binary
units (KiB, MiB, GiB, TiB = powers of 1024).

Samples are appended to `$XDG_STATE_HOME/sizewatch/history.jsonl` (default
`~/.local/state/sizewatch/history.jsonl`); set `SIZEWATCH_HISTORY` or pass
`--history FILE` to use another file.
`once` and `watch` keep it bounded: samples older than `--keep` (default
`30d`) are dropped, and samples older than a day are thinned to one per path
per hour.

Sizes are allocated disk usage (like `du`), in decimal units (1 GB = 10^9
bytes). Pass `--apparent` to sum file sizes instead. Hard-linked files are
counted once and symlinks are never followed. `--exclude GLOB` (repeatable)
skips matching entries: a pattern without a slash matches names at any depth
(`--exclude '*.tmp' --exclude .git`), one with a slash matches the path
relative to the measured folder (`--exclude 'build/cache'`).
`--one-file-system` stays on the folder's device, like `du -x`.

## License

MIT
