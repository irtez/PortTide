package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type TestProgress struct {
	Phase           string    `json:"phase"`
	StartedAt       time.Time `json:"startedAt"`
	DurationSeconds float64   `json:"durationSeconds,omitempty"`
	Completed       int       `json:"completed,omitempty"`
	Total           int       `json:"total,omitempty"`
}
type progressReporter func(TestProgress)

func reportStage(report progressReporter, phase string, duration time.Duration, completed, total int) {
	if report != nil {
		report(TestProgress{Phase: phase, StartedAt: time.Now().UTC(), DurationSeconds: duration.Seconds(), Completed: completed, Total: total})
	}
}

type speedOptions struct {
	Warmup, Duration, RequestTimeout           time.Duration
	Streams                                    int
	MaxBytes, InitialChunk, MinChunk, MaxChunk int64
}

func defaultSpeedOptions() speedOptions {
	return speedOptions{Warmup: 3 * time.Second, Duration: 20 * time.Second, RequestTimeout: 12 * time.Second, Streams: 4, MaxBytes: 1 << 30, InitialChunk: 8 << 10, MinChunk: 8 << 10, MaxChunk: 4 << 20}
}

type byteBudget struct {
	mu              sync.Mutex
	remaining, used int64
}

func (b *byteBudget) take(n int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining < 2 {
		return 0
	}
	if n > b.remaining {
		n = b.remaining
	}
	b.remaining -= n
	b.used += n
	return n
}

type phaseStats struct {
	bytes, traffic, samples, failures atomic.Int64
	mu                                sync.Mutex
	firstError                        string
	seconds                           float64
	used                              int64
	limited                           bool
	sizes                             []int64
}

func (s *phaseStats) failed(message string) {
	s.failures.Add(1)
	s.mu.Lock()
	if s.firstError == "" {
		s.firstError = message
	}
	s.mu.Unlock()
}

type countingReader struct {
	reader io.Reader
	bytes  atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, e := r.reader.Read(p)
	r.bytes.Add(int64(n))
	return n, e
}

var payloadOnce sync.Once
var payload []byte

func uploadPayload() []byte {
	payloadOnce.Do(func() {
		payload = make([]byte, 64<<10)
		if _, e := rand.Read(payload); e != nil {
			panic(e)
		}
	})
	return payload
}

type repeatedPayload struct {
	data   []byte
	offset int
}

func (r *repeatedPayload) Read(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		n := copy(p, r.data[r.offset:])
		p = p[n:]
		r.offset = (r.offset + n) % len(r.data)
	}
	return total, nil
}

type transferResult struct {
	bytes, traffic int64
	seconds        float64
	complete       bool
	err            error
}

func transfer(ctx context.Context, c *http.Client, upload bool, size int64) transferResult {
	method, target := "GET", fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", size)
	var body io.Reader
	var counter *countingReader
	if upload {
		method, target = "POST", "https://speed.cloudflare.com/__up"
		counter = &countingReader{reader: io.LimitReader(&repeatedPayload{data: uploadPayload()}, size)}
		body = counter
	}
	req, _ := http.NewRequestWithContext(ctx, method, target, body)
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-store")
	if upload {
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	start := time.Now()
	resp, e := c.Do(req)
	result := transferResult{}
	if e != nil {
		if counter != nil {
			result.traffic = counter.bytes.Load()
		}
		result.err = fmt.Errorf("transfer failed or timed out")
		result.seconds = time.Since(start).Seconds()
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if counter != nil {
			result.traffic = counter.bytes.Load()
		}
		result.err = fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
		return result
	}
	if upload {
		_, e = io.Copy(io.Discard, io.LimitReader(resp.Body, 65537))
		result.traffic = counter.bytes.Load()
		if e == nil && result.traffic == size {
			result.bytes = size
			result.complete = true
		} else {
			result.err = fmt.Errorf("upload was not fully acknowledged")
		}
	}
	if !upload {
		n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, size+1))
		result.traffic = n
		if n > size {
			result.err = fmt.Errorf("download exceeded requested size: received %d of %d bytes", n, size)
		} else if readErr == nil && n == size {
			result.bytes = n
			result.complete = true
		} else if readErr != nil && ctx.Err() != nil {
			result.bytes = n
			result.err = ctx.Err()
		} else {
			result.err = fmt.Errorf("download incomplete: received %d of %d bytes, read error %v", n, size, readErr)
		}
	}
	result.seconds = time.Since(start).Seconds()
	return result
}
func runTransferPhase(ctx context.Context, c *http.Client, upload bool, o speedOptions, duration time.Duration, capBytes int64, sizes []int64, measured bool) *phaseStats {
	stats := &phaseStats{sizes: make([]int64, o.Streams)}
	copy(stats.sizes, sizes)
	budget := &byteBudget{remaining: capBytes}
	start := time.Now()
	end := start.Add(duration)
	phaseCtx := ctx
	cancel := func() {}
	if measured {
		phaseCtx, cancel = context.WithTimeout(ctx, duration)
	}
	defer cancel()
	var wg sync.WaitGroup
	for worker := 0; worker < o.Streams; worker++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			size := stats.sizes[index]
			if size < o.MinChunk {
				size = o.InitialChunk
			}
			for phaseCtx.Err() == nil && time.Now().Before(end) {
				// Reserve one extra download byte to detect a server exceeding its declared payload.
				overhead := int64(0)
				if !upload {
					overhead = 1
				}
				reserved := budget.take(size + overhead)
				if reserved <= overhead {
					break
				}
				n := reserved - overhead
				requestCtx, finish := context.WithTimeout(phaseCtx, o.RequestTimeout)
				attempt := transfer(requestCtx, c, upload, n)
				finish()
				stats.bytes.Add(attempt.bytes)
				stats.traffic.Add(attempt.traffic)
				if attempt.complete {
					stats.samples.Add(1)
					next := int64(float64(n) / max(attempt.seconds, 0.001))
					next = max(n/8, min(n*8, next))
					size = max(o.MinChunk, min(o.MaxChunk, next))
					stats.sizes[index] = size
				}
				if attempt.err != nil {
					if phaseCtx.Err() == nil {
						stats.failed(attempt.err.Error())
					}
					break
				}
			}
		}(worker)
	}
	wg.Wait()
	stats.seconds = time.Since(start).Seconds()
	stats.used = budget.used
	stats.limited = budget.remaining < 2
	return stats
}
func measureThroughput(ctx context.Context, c *http.Client, upload bool, o speedOptions, report progressReporter) Measurement {
	kind, target := "download", "https://speed.cloudflare.com/__down"
	if upload {
		kind, target = "upload", "https://speed.cloudflare.com/__up"
	}
	m := Measurement{Kind: kind, Name: "Cloudflare", Target: target, Streams: o.Streams}
	sizes := make([]int64, o.Streams)
	for i := range sizes {
		sizes[i] = o.InitialChunk
	}
	reportStage(report, kind+"-warmup", o.Warmup, 0, 0)
	warm := runTransferPhase(ctx, c, upload, o, o.Warmup, min(int64(64<<20), o.MaxBytes/8), sizes, false)
	m.WarmupBytes = warm.bytes.Load()
	m.WarmupSeconds = warm.seconds
	m.TrafficBytes = warm.traffic.Load()
	if ctx.Err() != nil {
		m.Error = "test canceled"
		return m
	}
	if warm.samples.Load() == 0 {
		m.Error = warm.firstError
		if m.Error == "" {
			m.Error = "no successful warmup transfer"
		}
		return m
	}
	reportStage(report, kind, o.Duration, 0, 0)
	measured := runTransferPhase(ctx, c, upload, o, o.Duration, o.MaxBytes-warm.used, warm.sizes, true)
	m.Bytes = measured.bytes.Load()
	m.TrafficBytes += measured.traffic.Load()
	m.Seconds = measured.seconds
	m.Samples = int(measured.samples.Load())
	m.Failures = int(measured.failures.Load())
	if ctx.Err() != nil {
		m.Error = "test canceled"
		return m
	}
	if m.Bytes == 0 || m.Seconds <= 0 {
		m.Error = measured.firstError
		if m.Error == "" {
			m.Error = "no acknowledged payload measured"
		}
		return m
	}
	if m.Failures >= o.Streams {
		m.Error = measured.firstError + "; all transfer streams failed"
		return m
	}
	m.Value = float64(m.Bytes) * 8 / m.Seconds / 1e6
	m.Limit = "time"
	if measured.limited {
		m.Limit = "bytes"
		m.Warning = "Лимит теста: 1 ГиБ"
	}
	if m.Failures > 0 {
		m.Partial = true
		m.Warning = fmt.Sprintf("Неполный замер: %d потоков завершились с ошибкой (%s)", m.Failures, measured.firstError)
	}
	if measured.seconds < min(5.0, o.Duration.Seconds()/2) {
		m.Warning = "Короткая выборка; повторите проверку для сравнения. " + m.Warning
	}
	return m
}
func speedWithProgress(ctx context.Context, c *http.Client, report progressReporter) []Measurement {
	result := []Measurement{}
	for _, upload := range []bool{false, true} {
		result = append(result, measureThroughput(ctx, c, upload, defaultSpeedOptions(), report))
		if ctx.Err() != nil {
			break
		}
	}
	return result
}
func speed(ctx context.Context, c *http.Client) []Measurement { return speedWithProgress(ctx, c, nil) }
