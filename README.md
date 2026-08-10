# sizewatch

Tracks folder growth and runs a hook on spikes.

This first cut only measures: `sizewatch du` walks a tree concurrently and
reports how much disk space it actually uses.

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

Sizes are allocated disk usage (like `du`), in decimal units (1 GB = 10^9
bytes). Pass `--apparent` to sum file sizes instead. Hard-linked files are
counted once and symlinks are never followed.

## License

MIT
