package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func shortSpeedOptions() speedOptions {
	return speedOptions{Warmup: 20 * time.Millisecond, Duration: 80 * time.Millisecond, RequestTimeout: time.Second, Streams: 3, MaxBytes: 128 << 10, InitialChunk: 256, MinChunk: 128, MaxChunk: 2048}
}
func controlledTransfer(delay time.Duration, peak *atomic.Int64, active *atomic.Int64) *http.Client {
	return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			p := peak.Load()
			if p >= n || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(delay):
		}
		if r.Method == "POST" {
			n, e := io.Copy(io.Discard, r.Body)
			if e != nil {
				return nil, e
			}
			if n != r.ContentLength {
				return nil, errors.New("short request")
			}
			return response("ok"), nil
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
		return response(strings.Repeat("x", size)), nil
	})}
}
func TestSpeedUsesParallelTransfersAndExcludesWarmup(t *testing.T) {
	var peak, active atomic.Int64
	o := shortSpeedOptions()
	c := controlledTransfer(3*time.Millisecond, &peak, &active)
	for _, upload := range []bool{false, true} {
		m := measureThroughput(context.Background(), c, upload, o, nil)
		if m.Error != "" || m.Bytes == 0 || m.WarmupBytes == 0 || m.Value <= 0 || m.Streams != o.Streams {
			t.Fatalf("invalid measurement: %+v", m)
		}
		if m.TrafficBytes > o.MaxBytes || m.Bytes+m.WarmupBytes > m.TrafficBytes {
			t.Fatalf("budget/accounting failure: %+v", m)
		}
		if m.Seconds < 0.01 {
			t.Fatal("measurement window was skipped")
		}
	}
	if peak.Load() < 2 {
		t.Fatal("requests were serialized")
	}
}
func TestUploadRequiresServerAcknowledgement(t *testing.T) {
	o := shortSpeedOptions()
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { return response("ok"), nil })}
	m := measureThroughput(context.Background(), c, true, o, nil)
	if m.Error == "" || m.Bytes != 0 || m.Value != 0 {
		t.Fatalf("unread upload counted: %+v", m)
	}
}
func TestSpeedRejectsOversizedOrShortDownload(t *testing.T) {
	for _, delta := range []int{-1, 1} {
		o := shortSpeedOptions()
		c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			size, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
			return response(strings.Repeat("x", size+delta)), nil
		})}
		m := measureThroughput(context.Background(), c, false, o, nil)
		if m.Error == "" || m.Value != 0 {
			t.Fatalf("wrong payload size accepted: %+v", m)
		}
	}
}
func TestSpeedCancellationStopsRequests(t *testing.T) {
	o := shortSpeedOptions()
	o.Warmup = time.Second
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	m := measureThroughput(ctx, c, false, o, nil)
	if time.Since(start) > time.Second || m.Value != 0 {
		t.Fatalf("cancellation failed: %+v", m)
	}
}
func TestSpeedCapsPayloadAcrossWarmupAndMeasurement(t *testing.T) {
	var peak, active atomic.Int64
	o := shortSpeedOptions()
	o.MaxBytes = 16 << 10
	o.Duration = time.Second
	m := measureThroughput(context.Background(), controlledTransfer(time.Millisecond, &peak, &active), false, o, nil)
	if m.TrafficBytes > o.MaxBytes || m.Limit != "bytes" {
		t.Fatalf("payload cap missing: %+v", m)
	}
}
func TestLatencyWarmsUpThenReportsStableSamples(t *testing.T) {
	var calls atomic.Int64
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { calls.Add(1); return response(""), nil })}
	ms := latencyWithProgress(context.Background(), c, nil)
	if calls.Load() != 8 {
		t.Fatalf("got %d requests; want warmup + 7", calls.Load())
	}
	last := ms[len(ms)-1]
	if last.Name != "Median" || last.Samples != 7 || last.Failures != 0 {
		t.Fatalf("invalid summary: %+v", last)
	}
}

func TestAllTransferStreamsFailingCannotProduceSpeed(t *testing.T) {
	o := shortSpeedOptions()
	var measured atomic.Bool
	var calls atomic.Int64
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if measured.Load() && calls.Add(1) > int64(o.Streams) {
			res := response("denied")
			res.StatusCode = 403
			return res, nil
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
		time.Sleep(time.Millisecond)
		return response(strings.Repeat("x", size)), nil
	})}
	m := measureThroughput(context.Background(), c, false, o, func(p TestProgress) {
		if p.Phase == "download" {
			measured.Store(true)
		}
	})
	if m.Error == "" || m.Value != 0 {
		t.Fatalf("failed measurement presented a rate: %+v", m)
	}
}
