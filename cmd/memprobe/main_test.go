// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInvalidOptions(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-mode", "generate"},
		{"-mode", "other"},
		{"-mode", "baseline", "-input", "ignored.jpg"},
		{"-mode", "baseline", "extra"},
		{"-mode", "baseline", "-unknown"},
		{"-mode", "baseline", "-width", "0"},
		{"-mode", "baseline", "-width", "65536"},
		{"-mode", "baseline", "-width", "bad"},
		{"-mode", "baseline", "-workers", "0"},
		{"-mode", "baseline", "-workers", "65"},
		{"-mode", "baseline", "-max-memory", "-1"},
		{"-mode", "baseline", "-max-memory", "9223372036854775808"},
		{"-mode", "generate", "-input", "unused.jpg", "-size", "0x1"},
		{"-mode", "generate", "-input", "unused.jpg", "-size", "1X1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			if err := run(args, &out); err == nil {
				t.Fatal("accepted invalid options")
			}
			if out.Len() != 0 {
				t.Fatalf("invalid options wrote stdout: %q", &out)
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	o, err := parseOptions([]string{"-input", "giant.jpg"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.mode != "decode" || o.width != 256 || o.workers != 2 || o.maxMemory != 0 || o.sourceW != 20000 || o.sourceH != 15000 {
		t.Fatalf("unexpected defaults: %+v", o)
	}
	o, err = parseOptions([]string{"-mode", "baseline", "-max-memory", "9223372036854775807"}, io.Discard)
	if err != nil || o.maxMemory != 1<<63-1 {
		t.Fatalf("max int64 budget: %+v, %v", o, err)
	}
}

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-help"}, &out); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help error = %v", err)
	}
	if !strings.Contains(out.String(), "-max-memory") || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("incomplete help: %q", &out)
	}
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("measurement requires Linux /proc/self/status")
	}
}

func readMeasurement(t *testing.T, out []byte) measurement {
	t.Helper()
	var record measurement
	if err := json.Unmarshal(out, &record); err != nil {
		t.Fatalf("invalid measurement JSON %q: %v", out, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"factor", "progressive", "components", "workerCount", "streaming", "deferred", "fallback", "plannedMemoryBytes"} {
		if _, ok := fields[key]; ok {
			t.Errorf("reported inaccessible decoder field %q", key)
		}
	}
	if record.GoVersion == "" || record.GOOS != "linux" || record.GOMAXPROCS < 1 || record.Baseline.HeapAlloc == 0 || record.RSSKiB == 0 || record.PeakRSSKiB < record.RSSKiB {
		t.Fatalf("incomplete or inconsistent measurement: %+v", record)
	}
	return record
}

func TestRunBaseline(t *testing.T) {
	requireLinux(t)
	var out bytes.Buffer
	if err := run([]string{"-mode", "baseline"}, &out); err != nil {
		t.Fatal(err)
	}
	r := readMeasurement(t, out.Bytes())
	if r.Mode != "baseline" || r.Input != "" || r.HeapSamples != 0 || r.TargetWidth != 0 || r.Endpoint != (heap{}) {
		t.Fatalf("baseline unexpectedly decoded or sampled: %+v", r)
	}
}

func TestRunGenerateAndDecode(t *testing.T) {
	requireLinux(t)
	for _, tt := range []struct {
		w, h, width, targetH int
	}{
		{37, 29, 11, 8}, // Floor, not round-to-nearest.
		{97, 1, 3, 1},   // Floor is clamped to at least one.
		{1, 5, 1, 5},
	} {
		t.Run(fmt.Sprintf("%dx%d", tt.w, tt.h), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tiny.jpg")
			var out bytes.Buffer
			if err := run([]string{"-mode", "generate", "-input", path, "-size", fmt.Sprintf("%dx%d", tt.w, tt.h)}, &out); err != nil {
				t.Fatal(err)
			}
			var generated struct {
				Mode                      string
				SourceWidth, SourceHeight int
				SourceBytes               int64
				Quality                   int
			}
			if err := json.Unmarshal(out.Bytes(), &generated); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if generated.Mode != "generate" || generated.SourceWidth != tt.w || generated.SourceHeight != tt.h || generated.SourceBytes != fi.Size() || generated.Quality != 80 {
				t.Fatalf("incorrect generation record: %+v", generated)
			}
			out.Reset()
			if err := run([]string{"-input", path, "-width", fmt.Sprint(tt.width)}, &out); err != nil {
				t.Fatal(err)
			}
			r := readMeasurement(t, out.Bytes())
			if r.Mode != "decode" || r.SourceWidth != tt.w || r.SourceHeight != tt.h || r.TargetWidth != tt.width || r.TargetHeight != tt.targetH || r.WorkersRequested != 2 || !r.AllowFullDecode || r.MaxSourcePixels != 0 || r.MaxMemoryBytes != 0 || r.Error != "" {
				t.Fatalf("incorrect decode record: %+v", r)
			}
			if r.HeapSamples < 1 || r.SampleIntervalNS != int64(time.Millisecond) || r.SampledPeak.HeapAlloc < r.Endpoint.HeapAlloc {
				t.Fatalf("invalid decode samples: %+v", r)
			}
		})
	}
}

func TestRunBudgetRejection(t *testing.T) {
	requireLinux(t)
	path := filepath.Join(t.TempDir(), "tiny.jpg")
	if _, err := generate(path, 37, 29); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"-input", path, "-width", "11", "-max-memory", "1"}, &out)
	if err == nil || !strings.Contains(err.Error(), "memory budget") {
		t.Fatalf("budget rejection = %v", err)
	}
	r := readMeasurement(t, out.Bytes())
	if r.Error != err.Error() || r.MaxMemoryBytes != 1 || r.HeapSamples < 1 {
		t.Fatalf("missing rejection measurement: %+v", r)
	}
}

func TestRunProgressive(t *testing.T) {
	requireLinux(t)
	path := filepath.Join("..", "..", "testdata", "video-001.progressive.jpeg")
	var out bytes.Buffer
	if err := run([]string{"-input", path, "-width", "16"}, &out); err != nil {
		t.Fatal(err)
	}
	r := readMeasurement(t, out.Bytes())
	if !r.AllowFullDecode || r.TargetWidth != 16 || r.Error != "" || r.HeapSamples < 1 {
		t.Fatalf("incorrect progressive decode record: %+v", r)
	}
}

func TestRunInputErrors(t *testing.T) {
	requireLinux(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "invalid.jpg")
	if err := os.WriteFile(bad, []byte("not JPEG"), 0600); err != nil {
		t.Fatal(err)
	}
	tiny := filepath.Join(dir, "tiny.jpg")
	if _, err := generate(tiny, 37, 29); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.jpg"), bad, tiny} {
		var out bytes.Buffer
		if err := run([]string{"-input", path}, &out); err == nil {
			t.Fatalf("accepted missing/invalid/undersized input %q", path)
		}
		if out.Len() != 0 {
			t.Fatalf("configuration failure wrote measurement: %q", &out)
		}
	}
}
