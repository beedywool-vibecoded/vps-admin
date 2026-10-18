package system

import (
	"context"
	"sync"
	"time"
)

const (
	sampleEvery = 5 * time.Second
	shortLen    = 720 // 1 hour of 5 second samples
	longEvery   = 5 * time.Minute
	longLen     = 288 // 24 hours of 5 minute averages
)

// Point is the compact form kept in history, enough to draw the dashboard charts.
type Point struct {
	T     int64   `json:"t"` // unix seconds
	CPU   float64 `json:"cpu"`
	Mem   float64 `json:"mem"` // percent
	NetRx float64 `json:"rx"`
	NetTx float64 `json:"tx"`
	Load  float64 `json:"load"`
}

// Collector samples the host every 5 seconds and keeps two ring buffers: the last hour
// at full resolution and the last day as 5 minute averages.
type Collector struct {
	mu     sync.RWMutex
	latest Snapshot
	short  ring
	long   ring
	acc    []Point

	subMu sync.Mutex
	subs  map[chan Snapshot]struct{}
}

type ring struct {
	buf  []Point
	next int
	full bool
}

func (r *ring) add(p Point, size int) {
	if r.buf == nil {
		r.buf = make([]Point, size)
	}
	r.buf[r.next] = p
	r.next = (r.next + 1) % size
	if r.next == 0 {
		r.full = true
	}
}

func (r *ring) slice() []Point {
	if !r.full {
		return append([]Point(nil), r.buf[:r.next]...)
	}
	return append(append([]Point(nil), r.buf[r.next:]...), r.buf[:r.next]...)
}

func NewCollector() *Collector {
	return &Collector{subs: map[chan Snapshot]struct{}{}}
}

func (c *Collector) Run(ctx context.Context) {
	prevCPU := readCPU()
	prevRx, prevTx := readNet()
	prevT := time.Now()
	disks := readDisks()
	lastDisks := prevT
	sample := func(now time.Time) {
		cpu := readCPU()
		rx, tx := readNet()
		dt := now.Sub(prevT).Seconds()
		// Disk usage changes slowly and statfs can be slow on network mounts.
		if now.Sub(lastDisks) >= time.Minute {
			disks, lastDisks = readDisks(), now
		}
		total, avail, swapT, swapF := readMem()
		c.record(Snapshot{
			Time: now, Uptime: readUptime(), CPU: cpuPercent(prevCPU, cpu), Load: readLoad(),
			MemTotal: total, MemUsed: total - avail, SwapTotal: swapT, SwapUsed: swapT - swapF,
			NetRx: rate(prevRx, rx, dt), NetTx: rate(prevTx, tx, dt), Disks: disks,
		})
		prevCPU, prevRx, prevTx, prevT = cpu, rx, tx, now
	}
	// A first sample after one second, so the dashboard has data right after a restart.
	select {
	case <-ctx.Done():
		return
	case now := <-time.After(time.Second):
		sample(now)
	}
	t := time.NewTicker(sampleEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			sample(now)
		}
	}
}

func rate(a, b uint64, dt float64) float64 {
	if b < a || dt <= 0 { // counter reset or interface gone
		return 0
	}
	return float64(b-a) / dt
}

func (c *Collector) record(s Snapshot) {
	p := Point{T: s.Time.Unix(), CPU: round1(s.CPU), NetRx: s.NetRx, NetTx: s.NetTx, Load: s.Load[0]}
	if s.MemTotal > 0 {
		p.Mem = round1(100 * float64(s.MemUsed) / float64(s.MemTotal))
	}
	c.mu.Lock()
	c.latest = s
	c.short.add(p, shortLen)
	c.acc = append(c.acc, p)
	if len(c.acc) > 0 && p.T-c.acc[0].T >= int64(longEvery/time.Second) {
		c.long.add(average(c.acc), longLen)
		c.acc = c.acc[:0]
	}
	c.mu.Unlock()

	c.subMu.Lock()
	for ch := range c.subs {
		select {
		case ch <- s:
		default: // slow client, drop the sample
		}
	}
	c.subMu.Unlock()
}

func average(ps []Point) Point {
	var a Point
	for _, p := range ps {
		a.CPU += p.CPU
		a.Mem += p.Mem
		a.NetRx += p.NetRx
		a.NetTx += p.NetTx
		a.Load += p.Load
	}
	n := float64(len(ps))
	return Point{T: ps[len(ps)-1].T, CPU: round1(a.CPU / n), Mem: round1(a.Mem / n), NetRx: a.NetRx / n, NetTx: a.NetTx / n, Load: a.Load / n}
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

func (c *Collector) Latest() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// History returns "1h" at 5 s resolution or "24h" at 5 min resolution.
func (c *Collector) History(span string) []Point {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if span == "24h" {
		return c.long.slice()
	}
	return c.short.slice()
}

// Subscribe streams every new snapshot until cancel is called.
func (c *Collector) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 4)
	c.subMu.Lock()
	c.subs[ch] = struct{}{}
	c.subMu.Unlock()
	return ch, func() {
		c.subMu.Lock()
		delete(c.subs, ch)
		c.subMu.Unlock()
	}
}
