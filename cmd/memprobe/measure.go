// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type measurement struct {
	Mode             string `json:"mode"`
	Input            string `json:"input,omitempty"`
	GoVersion        string `json:"goVersion"`
	GOOS             string `json:"goos"`
	GOARCH           string `json:"goarch"`
	GOMAXPROCS       int    `json:"gomaxprocs"`
	GOGCEnv          string `json:"gogcEnv"`
	GOMEMLIMITEnv    string `json:"gomemlimitEnv"`
	SourceWidth      int    `json:"sourceWidth"`
	SourceHeight     int    `json:"sourceHeight"`
	SourceBytes      int64  `json:"sourceBytes"`
	TargetWidth      int    `json:"targetWidth"`
	TargetHeight     int    `json:"targetHeight"`
	WorkersRequested int    `json:"workersRequested"`
	AllowFullDecode  bool   `json:"allowFullDecode"`
	MaxSourcePixels  uint64 `json:"maxSourcePixels"`
	ElapsedNS        int64  `json:"elapsedNs"`
	SampleIntervalNS int64  `json:"sampleIntervalNs"`
	HeapSamples      uint64 `json:"heapSamples"`
	BaselineRSSKiB   uint64 `json:"baselineRSSKiB"`
	BaselineHWMKiB   uint64 `json:"baselineHWMKiB"`
	RSSKiB           uint64 `json:"rssKiB"`
	PeakRSSKiB       uint64 `json:"peakRSSKiB"`
	Baseline         heap   `json:"baseline"`
	Endpoint         heap   `json:"endpoint"`
	SampledPeak      heap   `json:"sampledPeak"`
	Error            string `json:"error,omitempty"`

	MaxMemoryBytes int64 `json:"maxMemoryBytes"`
}

// All sizes in these Go snapshots are bytes; RSS fields explicitly use KiB.
// sampledPeak takes the independent maximum of each field, not one instant's
// mutually consistent snapshot. TotalAlloc is cumulative, NOT peak live heap.
type heap struct {
	HeapAlloc   uint64 `json:"heapAlloc"`
	HeapInuse   uint64 `json:"heapInuse"`
	HeapSys     uint64 `json:"heapSys"`
	HeapObjects uint64 `json:"heapObjects"`
	StackInuse  uint64 `json:"stackInuse"`
	Sys         uint64 `json:"sys"`
	TotalAlloc  uint64 `json:"totalAlloc"`
	NumGC       uint32 `json:"numGC"`
}

func heapSnapshot(m *runtime.MemStats) heap {
	return heap{
		HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse, HeapSys: m.HeapSys,
		HeapObjects: m.HeapObjects, StackInuse: m.StackInuse, Sys: m.Sys,
		TotalAlloc: m.TotalAlloc, NumGC: m.NumGC,
	}
}

type heapSamples struct {
	endpoint, peak heap
	count          uint64
}

// The goroutine exclusively owns the maxima. The returned stop function is
// idempotent, takes a final sample, and joins before exposing those values.
func startSampler(baseline runtime.MemStats) func() heapSamples {
	stop := make(chan struct{})
	done := make(chan struct{})
	ready := make(chan struct{})
	var result heapSamples
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		result.peak = heapSnapshot(&baseline)
		sample := func() {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			h := heapSnapshot(&m)
			result.endpoint = h
			p := &result.peak
			p.HeapAlloc = max(p.HeapAlloc, h.HeapAlloc)
			p.HeapInuse = max(p.HeapInuse, h.HeapInuse)
			p.HeapSys = max(p.HeapSys, h.HeapSys)
			p.HeapObjects = max(p.HeapObjects, h.HeapObjects)
			p.StackInuse = max(p.StackInuse, h.StackInuse)
			p.Sys = max(p.Sys, h.Sys)
			p.TotalAlloc = max(p.TotalAlloc, h.TotalAlloc)
			p.NumGC = max(p.NumGC, h.NumGC)
			result.count++
		}
		close(ready)
		for {
			select {
			case <-ticker.C:
				sample()
			case <-stop:
				sample()
				return
			}
		}
	}()
	<-ready // Install the ticker before the calling goroutine starts decoding.
	var once sync.Once
	return func() heapSamples {
		once.Do(func() {
			close(stop)
			<-done
		})
		return result
	}
}

func readRSS() (rss, hwm uint64, err error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return parseStatus(f)
}

func parseStatus(r io.Reader) (rss, hwm uint64, err error) {
	scanner := bufio.NewScanner(r)
	var haveRSS, haveHWM bool
	for scanner.Scan() {
		line := scanner.Text()
		key, value, _ := strings.Cut(line, ":")
		if key != "VmRSS" && key != "VmHWM" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != "kB" {
			return 0, 0, fmt.Errorf("invalid /proc/self/status entry %q", line)
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid /proc/self/status entry %q: %w", line, err)
		}
		if key == "VmRSS" {
			rss, haveRSS = n, true
		} else {
			hwm, haveHWM = n, true
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if !haveRSS || !haveHWM {
		return 0, 0, fmt.Errorf("/proc/self/status is missing VmRSS or VmHWM")
	}
	return rss, hwm, nil
}
