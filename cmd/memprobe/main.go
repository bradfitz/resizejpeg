// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command memprobe measures one trusted JPEG thumbnail in a fresh process.
// Fixture generation must run separately from baseline/decode measurements.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/resizejpeg"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "memprobe:", err)
		os.Exit(1)
	}
}

type options struct {
	mode, input, size string
	width, workers    int
	maxMemory         int64
	sourceW, sourceH  int
}

func parseOptions(args []string, stdout io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("memprobe", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Keep stdout exclusively JSON except for -help.
	fs.StringVar(&o.mode, "mode", "decode", "generate, decode, or baseline (one operation per process)")
	fs.StringVar(&o.input, "input", "", "existing JPEG to decode, or fresh output path to generate (required except for baseline)")
	fs.StringVar(&o.size, "size", "20000x15000", "WIDTHxHEIGHT for generation; no full source pixel buffer is allocated")
	fs.IntVar(&o.width, "width", 256, "thumbnail width; height is floor(sourceHeight*width/sourceWidth), at least 1")
	fs.IntVar(&o.workers, "workers", 2, "requested decoder workers, in [1, 64]")
	fs.Int64Var(&o.maxMemory, "max-memory", 0, "decode reservation budget in bytes; 0 is unlimited (not an RSS limit)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "Usage: memprobe -mode generate|decode|baseline [options]")
			fs.SetOutput(stdout)
			fs.PrintDefaults()
		}
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected positional arguments: %q", fs.Args())
	}
	switch o.mode {
	case "decode", "generate":
		if o.input == "" {
			return o, fmt.Errorf("-input is required for -mode %s; no fixture is generated implicitly", o.mode)
		}
	case "baseline":
		if o.input != "" {
			return o, fmt.Errorf("-mode baseline does not accept -input")
		}
	default:
		return o, fmt.Errorf("invalid -mode %q: want generate, decode, or baseline", o.mode)
	}
	if o.width < 1 || o.width > 65535 {
		return o, fmt.Errorf("-width must be in [1, 65535], got %d", o.width)
	}
	if o.workers < 1 || o.workers > 64 {
		return o, fmt.Errorf("-workers must be in [1, 64], got %d", o.workers)
	}
	if o.maxMemory < 0 {
		return o, fmt.Errorf("-max-memory must be a nonnegative int64, got %d", o.maxMemory)
	}
	var err error
	o.sourceW, o.sourceH, err = parseSize(o.size)
	return o, err
}

// run performs exactly one operation. Tests may invoke it repeatedly, but such
// invocations are not isolated process-peak memory measurements.
func run(args []string, stdout io.Writer) error {
	o, err := parseOptions(args, stdout)
	if err != nil {
		return err
	}
	if o.mode == "generate" {
		start := time.Now()
		n, err := generate(o.input, o.sourceW, o.sourceH)
		if err != nil {
			return err
		}
		// Generation is deliberately not reported as a memory measurement.
		return json.NewEncoder(stdout).Encode(struct {
			Mode         string `json:"mode"`
			Input        string `json:"input"`
			SourceWidth  int    `json:"sourceWidth"`
			SourceHeight int    `json:"sourceHeight"`
			SourceBytes  int64  `json:"sourceBytes"`
			Quality      int    `json:"quality"`
			ElapsedNS    int64  `json:"elapsedNs"`
		}{o.mode, o.input, o.sourceW, o.sourceH, n, 80, time.Since(start).Nanoseconds()})
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("memory measurement requires Linux /proc/self/status")
	}
	record := measurement{
		Mode: o.mode, GoVersion: runtime.Version(), GOOS: runtime.GOOS,
		GOARCH: runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0),
		GOGCEnv: os.Getenv("GOGC"), GOMEMLIMITEnv: os.Getenv("GOMEMLIMIT"),
		MaxMemoryBytes: o.maxMemory,
	}
	var reader *bufio.Reader
	if o.mode == "decode" {
		f, err := os.Open(o.input)
		if err != nil {
			return err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		reader = bufio.NewReader(f)
		cfg, err := resizejpeg.DecodeConfig(reader)
		if err != nil {
			return fmt.Errorf("decode config: %w", err)
		}
		if err := dimensions(cfg.Width, cfg.Height); err != nil {
			return err
		}
		if o.width > cfg.Width {
			return fmt.Errorf("target width %d exceeds source width %d", o.width, cfg.Width)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		// Discard bytes prefetched while reading the configuration.
		reader.Reset(f)
		record.Input, record.SourceBytes = o.input, fi.Size()
		record.SourceWidth, record.SourceHeight = cfg.Width, cfg.Height
		record.TargetWidth = o.width
		record.TargetHeight = max(1, int(uint64(cfg.Height)*uint64(o.width)/uint64(cfg.Width)))
		record.WorkersRequested = o.workers
		record.AllowFullDecode = true
		// MaxSourcePixels intentionally remains zero for trusted giants.
	}

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	record.Baseline = heapSnapshot(&baseline)
	record.BaselineRSSKiB, record.BaselineHWMKiB, err = readRSS()
	if err != nil {
		return err
	}
	if o.mode == "baseline" {
		record.RSSKiB, record.PeakRSSKiB = record.BaselineRSSKiB, record.BaselineHWMKiB
		return json.NewEncoder(stdout).Encode(record)
	}

	stop := startSampler(baseline)
	defer stop() // Idempotent; always join even if a later operation fails.
	record.SampleIntervalNS = int64(time.Millisecond)
	start := time.Now()
	im, decodeErr := resizejpeg.DecodeThumbnail(reader, record.TargetWidth, record.TargetHeight, &resizejpeg.ResizeOptions{
		Workers: record.WorkersRequested, AllowFullDecode: true, MaxSourcePixels: 0,
		MaxMemoryBytes: record.MaxMemoryBytes,
		// No MemAllocSemaphore: do not change the decoder's buffer cleanup.
	})
	record.ElapsedNS = time.Since(start).Nanoseconds()
	samples := stop()
	record.Endpoint, record.SampledPeak, record.HeapSamples = samples.endpoint, samples.peak, samples.count
	record.RSSKiB, record.PeakRSSKiB, err = readRSS()
	// The public API exposes only the returned thumbnail, not its decoder.
	// Historical in-package probes also kept decoder scratch alive here.
	runtime.KeepAlive(im)
	runtime.KeepAlive(reader)
	if err != nil {
		return err
	}
	if decodeErr == nil && (im == nil || im.Bounds().Size() != image.Pt(record.TargetWidth, record.TargetHeight)) {
		decodeErr = fmt.Errorf("decoder returned unexpected thumbnail bounds")
	}
	if decodeErr != nil {
		record.Error = decodeErr.Error()
	}
	if err := json.NewEncoder(stdout).Encode(record); err != nil {
		return err
	}
	return decodeErr
}

func dimensions(w, h int) error {
	if w < 1 || w > 65535 || h < 1 || h > 65535 {
		return fmt.Errorf("JPEG dimensions must be in [1, 65535], got %dx%d", w, h)
	}
	return nil
}

func parseSize(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "x")
	w, ew := strconv.Atoi(a)
	h, eh := strconv.Atoi(b)
	if !ok || ew != nil || eh != nil {
		return 0, 0, fmt.Errorf("-size must be WIDTHxHEIGHT, got %q", s)
	}
	if err := dimensions(w, h); err != nil {
		return 0, 0, err
	}
	return w, h, nil
}
