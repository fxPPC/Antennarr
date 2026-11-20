// Package systeminfo samples host load, not per-process CPU consumption. A mode
// is assigned only when streaming or actually checking; idle samples are ignored.
package systeminfo

import (
	"context"
	"runtime"
	"sync"
	"time"
)

type sample struct {
	At        time.Time
	Load      float64
	Streaming bool
}
type Average struct {
	Load    float64 `json:"load"`
	Samples int     `json:"samples"`
}
type Snapshot struct {
	Uname         string  `json:"uname"`
	Go            string  `json:"go"`
	CPUs          int     `json:"cpus"`
	Uptime        int64   `json:"uptime_seconds"`
	DiskFree      uint64  `json:"disk_free_bytes"`
	DiskTotal     uint64  `json:"disk_total_bytes"`
	Background    Average `json:"background"`
	Streaming     Average `json:"streaming"`
	WindowMinutes int     `json:"window_minutes"`
	Note          string  `json:"note"`
}
type Collector struct {
	mu      sync.Mutex
	started time.Time
	uname   string
	samples []sample
	Root    string
	Mode    func() (streaming, checking bool)
}

func New(root string, mode func() (bool, bool)) *Collector {
	return &Collector{Root: root, Mode: mode, started: time.Now(), uname: systemName()}
}
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect()
		}
	}
}
func (c *Collector) collect() {
	streaming, checking := c.Mode()
	if !streaming && !checking {
		return
	}
	load, ok := hostLoad()
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples = append(c.samples, sample{time.Now(), load, streaming})
	cut := time.Now().Add(-time.Hour)
	i := 0
	for i < len(c.samples) && c.samples[i].At.Before(cut) {
		i++
	}
	c.samples = append([]sample(nil), c.samples[i:]...)
}
func (c *Collector) Snapshot() Snapshot {
	v := Snapshot{Uname: c.uname, Go: runtime.Version(), CPUs: runtime.NumCPU(), Uptime: int64(time.Since(c.started).Seconds()), WindowMinutes: 60, Note: "Host 1-minute load sampled every 10 seconds since startup, up to the past hour; includes other host workloads. No samples means no measurement yet."}
	if runtime.GOOS == "windows" {
		v.Note = "Windows does not report a Unix-style one-minute host load average."
	}
	v.DiskFree, v.DiskTotal = diskSpace(c.Root)
	c.mu.Lock()
	defer c.mu.Unlock()
	cut := time.Now().Add(-time.Hour)
	for _, s := range c.samples {
		if s.At.Before(cut) {
			continue
		}
		a := &v.Background
		if s.Streaming {
			a = &v.Streaming
		}
		a.Load += s.Load
		a.Samples++
	}
	if v.Background.Samples > 0 {
		v.Background.Load /= float64(v.Background.Samples)
	}
	if v.Streaming.Samples > 0 {
		v.Streaming.Load /= float64(v.Streaming.Samples)
	}
	return v
}
