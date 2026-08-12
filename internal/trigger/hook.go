package trigger

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/units"
)

// Event is one alert: a path whose new sample fired at least one trigger.
type Event struct {
	Cur     history.Sample
	Prev    *history.Sample // nil on the first sample of a path
	Reasons []Reason        // never empty
}

// Delta is the growth since the previous sample, or 0 without one.
func (e Event) Delta() int64 {
	if e.Prev == nil {
		return 0
	}
	return e.Cur.Bytes - e.Prev.Bytes
}

// Env returns the variables describing the event to a hook.
// SIZEWATCH_PREV_BYTES is empty when there is no previous sample.
func (e Event) Env() []string {
	prev := ""
	if e.Prev != nil {
		prev = strconv.FormatInt(e.Prev.Bytes, 10)
	}
	reasons := make([]string, len(e.Reasons))
	for i, r := range e.Reasons {
		reasons[i] = string(r)
	}
	return []string{
		"SIZEWATCH_PATH=" + e.Cur.Path,
		"SIZEWATCH_BYTES=" + strconv.FormatInt(e.Cur.Bytes, 10),
		"SIZEWATCH_BYTES_HUMAN=" + units.FormatSize(e.Cur.Bytes),
		"SIZEWATCH_PREV_BYTES=" + prev,
		"SIZEWATCH_DELTA_BYTES=" + strconv.FormatInt(e.Delta(), 10),
		"SIZEWATCH_DELTA_HUMAN=" + units.FormatDelta(e.Delta()),
		"SIZEWATCH_REASON=" + reasons[0],
		"SIZEWATCH_REASONS=" + strings.Join(reasons, ","),
	}
}

// RunHook runs command with sh -c and the event in its environment, and
// waits for it. The hook's output goes to stdout and stderr.
func RunHook(ctx context.Context, command string, ev Event, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), ev.Env()...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// A hook that backgrounds a child may leave our pipes open; don't wait
	// on that child forever.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook: %w", err)
	}
	return nil
}
