package walk

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Random bytes so compressing filesystems can't shrink the allocation.
	data := make([]byte, n)
	r := rand.NewChaCha8([32]byte{})
	_, _ = r.Read(data)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hardlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Link(oldname, newname); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}
}

func symlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
}

// lsize is the apparent size of path itself, without following symlinks.
func lsize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func measure(t *testing.T, root string, opts Options) Result {
	t.Helper()
	res, err := Measure(context.Background(), root, opts)
	if err != nil {
		t.Fatalf("Measure(%s): %v", root, err)
	}
	return res
}

// TestMeasureLinks builds a tree with hard links and symlinks (one pointing
// at a large directory outside the tree) and checks the apparent total.
func TestMeasureLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "big"), 1<<20)

	writeFile(t, filepath.Join(root, "a.txt"), 1000)
	writeFile(t, filepath.Join(root, "sub", "b.bin"), 5000)
	writeFile(t, filepath.Join(root, "sub", "deep", "c.log"), 300)
	hardlink(t, filepath.Join(root, "sub", "b.bin"), filepath.Join(root, "sub", "b.again"))
	hardlink(t, filepath.Join(root, "a.txt"), filepath.Join(root, "sub", "deep", "a.again"))
	links := []string{
		filepath.Join(root, "dirlink"),
		filepath.Join(root, "outside"),
		filepath.Join(root, "filelink"),
	}
	symlink(t, "sub", links[0])
	symlink(t, outside, links[1])
	symlink(t, "a.txt", links[2])

	want := int64(1000 + 5000 + 300)
	for _, p := range []string{root, filepath.Join(root, "sub"), filepath.Join(root, "sub", "deep")} {
		want += lsize(t, p)
	}
	for _, p := range links {
		want += lsize(t, p)
	}

	for _, workers := range []int{1, 2, 32} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			res := measure(t, root, Options{Apparent: true, Workers: workers})
			if res.Bytes != want {
				t.Errorf("Bytes = %d, want %d", res.Bytes, want)
			}
			// a.txt, b.bin, c.log and the three symlinks; hard links once.
			if res.Files != 6 {
				t.Errorf("Files = %d, want 6", res.Files)
			}
			if res.Dirs != 3 {
				t.Errorf("Dirs = %d, want 3", res.Dirs)
			}
			if res.Warnings != 0 {
				t.Errorf("Warnings = %d, want 0", res.Warnings)
			}
		})
	}
}

func TestMeasureAllocatedHardLinkCountedOnce(t *testing.T) {
	root := t.TempDir()
	blob := filepath.Join(root, "data", "blob")
	writeFile(t, blob, 1<<20)
	if err := os.Mkdir(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}

	before := measure(t, root, Options{})
	if before.Bytes < 1<<20 {
		t.Fatalf("allocated %d bytes for a 1 MiB file", before.Bytes)
	}
	hardlink(t, blob, filepath.Join(root, "other", "blob"))
	hardlink(t, blob, filepath.Join(root, "data", "blob2"))
	after := measure(t, root, Options{})

	// Directory blocks may grow a little; a second copy of the file may not.
	if grew := after.Bytes - before.Bytes; grew >= 1<<19 {
		t.Errorf("hard links added %d bytes (before %d, after %d)", grew, before.Bytes, after.Bytes)
	}
	if after.Files != 1 {
		t.Errorf("Files = %d, want 1", after.Files)
	}
}

func TestMeasureManyDirsMatchesSerial(t *testing.T) {
	root := t.TempDir()
	for i := range 40 {
		for j := range 3 {
			writeFile(t, filepath.Join(root, fmt.Sprintf("d%02d", i), fmt.Sprintf("e%d", j), "f"), 100*i+j)
		}
	}
	serial := measure(t, root, Options{Workers: 1})
	for range 5 {
		got := measure(t, root, Options{Workers: 16})
		if got != serial {
			t.Fatalf("concurrent walk %+v != serial walk %+v", got, serial)
		}
	}
	if serial.Files != 120 || serial.Dirs != 1+40+120 {
		t.Errorf("serial walk counted %d files, %d dirs", serial.Files, serial.Dirs)
	}
}

func TestMeasureSymlinkRootIsResolved(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "real")
	writeFile(t, filepath.Join(root, "x"), 4096)
	link := filepath.Join(dir, "link")
	symlink(t, root, link)

	want := measure(t, root, Options{Apparent: true})
	got := measure(t, link, Options{Apparent: true})
	if got.Bytes != want.Bytes || got.Files != want.Files {
		t.Errorf("via symlink: %+v, direct: %+v", got, want)
	}
	if got.Path != link {
		t.Errorf("Path = %q, want %q", got.Path, link)
	}
}

func TestMeasureSingleFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, 1234)
	res := measure(t, f, Options{Apparent: true})
	if res.Bytes != 1234 || res.Files != 1 || res.Dirs != 0 {
		t.Errorf("got %+v", res)
	}
}

func TestMeasureMissingRoot(t *testing.T) {
	_, err := Measure(context.Background(), filepath.Join(t.TempDir(), "nope"), Options{})
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want not-exist", err)
	}
}

func TestMeasureCanceled(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "b"), 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Measure(ctx, root, Options{}); err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestMeasureSkipsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for this user")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ok", "f"), 10)
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "secret"), 10)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	res := measure(t, root, Options{Apparent: true})
	if res.Warnings != 1 {
		t.Errorf("Warnings = %d, want 1", res.Warnings)
	}
	if res.Files != 1 {
		t.Errorf("Files = %d, want 1 (the readable one)", res.Files)
	}
}
