// Package walk measures the disk usage of a directory tree.
package walk

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Options controls how a tree is measured.
type Options struct {
	// Apparent counts the sizes of regular files and symlinks (st_size)
	// instead of allocated disk blocks. Sparse files then count in full,
	// small files count less and directories count nothing.
	Apparent bool
	// Exclude holds glob patterns (path.Match syntax) of entries to skip.
	// A pattern without a slash is matched against each entry's name, so
	// "*.tmp" or "node_modules" match at any depth. A pattern with a slash
	// is matched against the slash-separated path relative to the root,
	// e.g. "build/cache" or "*/node_modules".
	Exclude []string
	// OneFileSystem skips directories on a different device than the
	// root, like du -x.
	OneFileSystem bool
	// Workers bounds how many goroutines walk the tree at once.
	// Zero or less means DefaultWorkers().
	Workers int
}

// Result is the outcome of measuring one tree.
type Result struct {
	Path     string // the root as given to Measure
	Bytes    int64  // allocated (or apparent) bytes under the root
	Files    int64  // non-directory entries, hard links counted once
	Dirs     int64  // directories, including the root
	Warnings int64  // entries that could not be read and were skipped
}

// DefaultWorkers is the walker pool size used when Options.Workers is unset.
// Walking is dominated by stat calls, so it pays to run more walkers than CPUs.
func DefaultWorkers() int {
	return min(max(2*runtime.GOMAXPROCS(0), 4), 32)
}

// stat is the platform-specific part of a file's metadata.
type stat struct {
	dev, ino, nlink uint64
	blocks          int64 // 512-byte blocks
}

type fileID struct{ dev, ino uint64 }

type counts struct{ bytes, files, dirs, warnings int64 }

type walker struct {
	ctx  context.Context
	root string
	dev  uint64 // device of root, for OneFileSystem
	opts Options
	sem  chan struct{} // one token per running walk goroutine
	wg   sync.WaitGroup

	mu   sync.Mutex
	seen map[fileID]struct{} // hard-linked files already counted

	bytes, files, dirs, warnings atomic.Int64

	rootErr atomic.Pointer[error]
}

// Measure walks root and returns the disk space it uses.
//
// Symbolic links inside the tree are never followed (the link itself is
// counted); if root itself is a symlink it is resolved first. Files with
// several hard links are counted once. Entries that cannot be read are
// skipped and counted in Result.Warnings; only an unreadable root is an error.
func Measure(ctx context.Context, root string, opts Options) (Result, error) {
	res := Result{Path: root}
	start, err := filepath.EvalSymlinks(root)
	if err != nil {
		return res, err
	}
	for _, pat := range opts.Exclude {
		if err := ValidatePattern(pat); err != nil {
			return res, err
		}
	}
	if opts.Workers <= 0 {
		opts.Workers = DefaultWorkers()
	}
	w := &walker{
		ctx:  ctx,
		root: start,
		opts: opts,
		sem:  make(chan struct{}, opts.Workers),
		seen: make(map[fileID]struct{}),
	}
	if opts.OneFileSystem {
		fi, err := os.Lstat(start)
		if err != nil {
			return res, err
		}
		if st, ok := statOf(fi); ok {
			w.dev = st.dev
		} else {
			w.opts.OneFileSystem = false // no device numbers to compare
		}
	}
	w.sem <- struct{}{}
	w.wg.Add(1)
	go w.walk(start)
	w.wg.Wait()

	if err := ctx.Err(); err != nil {
		return res, err
	}
	if p := w.rootErr.Load(); p != nil {
		return res, *p
	}
	res.Bytes = w.bytes.Load()
	res.Files = w.files.Load()
	res.Dirs = w.dirs.Load()
	res.Warnings = w.warnings.Load()
	return res, nil
}

// walk measures the subtree at dir with filepath.WalkDir. Whenever it meets a
// subdirectory while a pool slot is free, it hands that subtree to a new
// goroutine instead of descending itself, so the pool stays busy without
// ever exceeding opts.Workers goroutines.
func (w *walker) walk(dir string) {
	defer w.wg.Done()
	defer func() { <-w.sem }()

	var c counts
	defer w.add(&c)

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if cerr := w.ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if path == w.root {
				w.rootErr.CompareAndSwap(nil, &err)
				return err
			}
			// Unreadable directory or vanished entry: note it and carry on.
			c.warnings++
			return nil
		}
		// The start of a spawned walk was already vetted by its parent.
		if path != dir && w.excluded(path, d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			c.warnings++
			return nil
		}
		st, hasStat := statOf(info)
		if d.IsDir() {
			if path != dir && w.opts.OneFileSystem && hasStat && st.dev != w.dev {
				return filepath.SkipDir
			}
			if path != dir && w.spawn(path) {
				return filepath.SkipDir
			}
			c.dirs++
			c.bytes += w.size(info, st, hasStat)
			return nil
		}
		if hasStat && st.nlink > 1 && !w.firstSighting(st) {
			return nil
		}
		c.files++
		c.bytes += w.size(info, st, hasStat)
		return nil
	})
}

// excluded reports whether an entry matches one of the exclude patterns.
func (w *walker) excluded(p, name string) bool {
	var rel string
	for _, pat := range w.opts.Exclude {
		pat = strings.TrimSuffix(strings.TrimPrefix(pat, "./"), "/")
		if !strings.Contains(pat, "/") {
			if ok, _ := path.Match(pat, name); ok {
				return true
			}
			continue
		}
		if rel == "" {
			r, err := filepath.Rel(w.root, p)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(r)
		}
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
	}
	return false
}

// ValidatePattern reports whether pat is a usable exclude pattern.
func ValidatePattern(pat string) error {
	if strings.TrimSuffix(strings.TrimPrefix(pat, "./"), "/") == "" {
		return fmt.Errorf("empty exclude pattern %q", pat)
	}
	if _, err := path.Match(pat, ""); err != nil {
		return fmt.Errorf("bad exclude pattern %q: %w", pat, err)
	}
	return nil
}

// spawn starts a goroutine walking dir if the pool has room.
func (w *walker) spawn(dir string) bool {
	select {
	case w.sem <- struct{}{}:
		w.wg.Add(1)
		go w.walk(dir)
		return true
	default:
		return false
	}
}

func (w *walker) size(info fs.FileInfo, st stat, hasStat bool) int64 {
	if w.opts.Apparent || !hasStat {
		// Like du --apparent-size: the st_size of directories and devices
		// is filesystem-specific bookkeeping, not data.
		if info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
			return info.Size()
		}
		return 0
	}
	return st.blocks * 512
}

// firstSighting reports whether this is the first time the walk has seen
// the inode behind a hard-linked file.
func (w *walker) firstSighting(st stat) bool {
	id := fileID{st.dev, st.ino}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, dup := w.seen[id]; dup {
		return false
	}
	w.seen[id] = struct{}{}
	return true
}

func (w *walker) add(c *counts) {
	w.bytes.Add(c.bytes)
	w.files.Add(c.files)
	w.dirs.Add(c.dirs)
	w.warnings.Add(c.warnings)
}
