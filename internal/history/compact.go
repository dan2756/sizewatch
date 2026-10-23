package history

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ThinAfter is the age beyond which Compact keeps one sample per path per
// hour. Recent history stays at full resolution for report's 1h figure and
// sparkline; older samples only feed the 24h and 7d figures.
const ThinAfter = 24 * time.Hour

// Compact returns the samples worth keeping at now. Samples older than keep
// are dropped (keep <= 0 drops none by age) and samples older than ThinAfter
// are thinned to the latest one per path per hour, whatever keep is. The
// input is not modified; the result is sorted oldest first.
func Compact(samples []Sample, now time.Time, keep time.Duration) []Sample {
	sorted := slices.Clone(samples)
	sortByTime(sorted)

	type bucket struct {
		path string
		hour int64
	}
	slot := make(map[bucket]int) // index in out of the bucket's sample
	out := make([]Sample, 0, len(sorted))
	for _, s := range sorted {
		age := now.Sub(s.Time)
		if keep > 0 && age > keep {
			continue
		}
		if age > ThinAfter {
			b := bucket{s.Path, s.Time.Truncate(time.Hour).Unix()}
			if i, ok := slot[b]; ok {
				out[i] = s // later sample in the same hour wins
				continue
			}
			slot[b] = len(out)
		}
		out = append(out, s)
	}
	sortByTime(out)
	return out
}

// Compact applies Compact to the file and rewrites it, but only if that
// drops at least minDrop samples, so a large history isn't rewritten for
// the sake of one line. It returns how many samples were dropped.
func (s *Store) Compact(now time.Time, keep time.Duration, minDrop int) (int, error) {
	unlock, err := s.Lock()
	if err != nil {
		return 0, err
	}
	defer unlock()

	samples, err := s.Load()
	if err != nil {
		return 0, err
	}
	kept := Compact(samples, now, keep)
	dropped := len(samples) - len(kept)
	if dropped == 0 || dropped < minDrop {
		return 0, nil
	}
	if err := s.rewrite(kept); err != nil {
		return 0, fmt.Errorf("compacting %s: %w", s.path, err)
	}
	return dropped, nil
}

// rewrite atomically replaces the file with samples. The caller holds the
// lock.
func (s *Store) rewrite(samples []Sample) error {
	buf, err := encode(samples)
	if err != nil {
		return err
	}
	// Keep the mode the file already has. CreateTemp plus a forced 0644
	// would publish a history the user had made private (chmod 0600, or a
	// restrictive umask on the original append).
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(s.path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Lock serializes writers, which may be different sizewatch processes (a
// watch and a cron job, say), with an advisory lock on a side file. The
// history file can't hold the lock itself because compaction replaces it.
// Append and Compact take it; callers take it to update other state kept
// next to the history. It is not reentrant.
func (s *Store) Lock() (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking %s: %w", f.Name(), err)
	}
	return func() { f.Close() }, nil
}
