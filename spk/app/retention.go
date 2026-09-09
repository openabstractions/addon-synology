package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	job "github.com/openabstractions/abstraction-job/go"
)

type sweptCount struct {
	mu      sync.Mutex
	at      time.Time
	removed int
	freed   int64
	err     string
}

// keepSwept is a clock, not a poll. Everything else in this window waits on the
// layer's watch, because the layer knows when a record changed; nothing knows
// when a day has passed except a clock, and retention's only input is elapsed
// time.
func (a *app) keepSwept(every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if set, err := a.settings.read(); err == nil {
			a.sweepNow(set)
		}
		<-tick.C
	}
}

// sweepNow removes delivered files older than the policy and leaves every
// record exactly where it is.
//
// That split is the answer to what deleting a delivered file means for a record
// that says it was delivered: the record is the history of an operation, not an
// index of the filesystem. "These bytes were fetched, verified against this
// digest and written here" stays true forever; "that file is still there" was
// never what it claimed. So the bytes go and the record stays, and the window
// says "kept, file removed" rather than showing a path that is not there.
func (a *app) sweepNow(set Policy) {
	if set.KeepFilesDays <= 0 {
		a.swept.note(time.Now(), 0, 0, "")
		return
	}
	recs, err := a.jobs.List()
	if err != nil {
		a.swept.note(time.Now(), 0, 0, err.Error())
		return
	}
	cutoff := time.Now().AddDate(0, 0, -set.KeepFilesDays)
	removed, freed, worst := 0, int64(0), ""
	for _, rec := range recs {
		if rec.State != job.StateComplete || rec.UpdatedAt.Time.After(cutoff) {
			continue
		}
		dest, err := a.downloads.Open(rec.ID).Destination()
		if err != nil || !a.inStore(dest) {
			continue
		}
		info, err := os.Stat(dest)
		if err != nil || info.IsDir() {
			continue
		}
		if err := os.Remove(dest); err != nil {
			worst = err.Error()
			continue
		}
		removed++
		freed += info.Size()
	}
	a.swept.note(time.Now(), removed, freed, worst)
}

// inStore is the one guard between a policy in days and rm. A destination the
// layer resolved should already sit under the root; deleting on the strength of
// "should" is how a retention rule becomes an incident.
func (a *app) inStore(dest string) bool {
	if a.root == "" || dest == "" {
		return false
	}
	rel, err := filepath.Rel(a.root, dest)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (c *sweptCount) note(at time.Time, removed int, freed int64, err string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.err = at, err
	if removed > 0 {
		c.removed, c.freed = removed, freed
	}
}

func (c *sweptCount) read() (time.Time, int, int64, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at, c.removed, c.freed, c.err
}
