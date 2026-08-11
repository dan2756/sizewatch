# sizewatch

Tracks folder growth and runs a hook on spikes.

`sizewatch du` walks a tree concurrently and reports how much disk space it
actually uses; `sizewatch once` records a sample in a history file and shows
how much each folder changed since the last one.

## Install

```sh
go install github.com/dan2756/sizewatch@latest
```

## Usage

```sh
sizewatch du ~/Downloads ./node_modules
```

```
  12.4 GB  /Users/dj/Downloads
 412.0 MB  ./node_modules
```

```sh
sizewatch once ~/Downloads ./node_modules
```

```
/Users/dj/Downloads          12.4 GB   +600.0 MB in 5m
/Users/dj/proj/node_modules  412.0 MB  first sample
```

Samples are appended to `$XDG_STATE_HOME/sizewatch/history.jsonl` (default
`~/.local/state/sizewatch/history.jsonl`); set `SIZEWATCH_HISTORY` or pass
`--history FILE` to use another file.

Sizes are allocated disk usage (like `du`), in decimal units (1 GB = 10^9
bytes). Pass `--apparent` to sum file sizes instead. Hard-linked files are
counted once and symlinks are never followed.

## License

MIT
