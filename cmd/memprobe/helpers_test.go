// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHelpers(t *testing.T) {
	t.Run("lazyImage", func(t *testing.T) {
		m := newProbeImage(37, 29)
		if m.Bounds() != image.Rect(0, 0, 37, 29) || m.ColorModel() != color.YCbCrModel || !m.Opaque() {
			t.Fatal("incorrect image bounds, color model or opacity")
		}
		colors := make(map[color.Color]bool)
		for y := 0; y < 29; y++ {
			for x := 0; x < 37; x++ {
				c := m.At(x, y)
				_, _, _, a := c.RGBA()
				if a != 65535 {
					t.Fatal("non-opaque in-bounds pixel")
				}
				if _, ok := c.(color.YCbCr); !ok {
					t.Fatalf("pixel is %T, want color.YCbCr", c)
				}
				colors[c] = true
			}
		}
		if len(colors) < 64 {
			t.Fatalf("insufficient color/texture variety: %d colors", len(colors))
		}
		var pixel color.Color
		x := 0
		if n := testing.AllocsPerRun(1000, func() {
			pixel = m.At(x%37, (x/37)%29)
			x++
		}); n != 0 {
			t.Fatalf("At allocated %g times per pixel, want zero", n)
		}
		runtime.KeepAlive(pixel)
		_, _, _, a := m.At(-1, -1).RGBA()
		if a != 0 {
			t.Fatal("out-of-bounds pixel is not transparent")
		}
	})
	t.Run("size", func(t *testing.T) {
		for _, s := range []string{"1x1", "20000x15000", "65535x65535"} {
			if _, _, err := parseSize(s); err != nil {
				t.Errorf("%q: %v", s, err)
			}
		}
		for _, s := range []string{"", "1", "1X1", "1x1x1", "0x1", "1x0", "-1x2", "65536x1", "1x65536", "999999999999999999999x1"} {
			if _, _, err := parseSize(s); err == nil {
				t.Errorf("accepted invalid size %q", s)
			}
		}
	})
	t.Run("generation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "tiny.jpg")
		n, err := generate(path, 37, 29)
		if err != nil {
			t.Fatal(err)
		}
		if n <= 0 {
			t.Fatal("empty JPEG")
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		im, err := jpeg.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		ycc, ok := im.(*image.YCbCr)
		if !ok || ycc.Bounds() != image.Rect(0, 0, 37, 29) || ycc.SubsampleRatio != image.YCbCrSubsampleRatio420 {
			t.Fatalf("want three-component 4:2:0 37x29 JPEG, got %T", im)
		}
		if _, err := generate(path, 37, 29); !os.IsExist(err) {
			t.Fatalf("overwrite error = %v, want os.ErrExist", err)
		}
		fi, err := os.Stat(path)
		if err != nil || fi.Size() != n {
			t.Fatalf("original JPEG changed on overwrite attempt: %v", err)
		}
		if _, err := generate(filepath.Join(t.TempDir(), "bad.jpg"), 0, 1); err == nil {
			t.Fatal("accepted zero width")
		}
	})
	t.Run("procStatus", func(t *testing.T) {
		rss, hwm, err := parseStatus(strings.NewReader("Name:\ttest\nVmHWM:\t12345 kB\nVmRSS:\t6789 kB\nThreads:\t2\n"))
		if err != nil || rss != 6789 || hwm != 12345 {
			t.Fatalf("RSS=%d HWM=%d error=%v", rss, hwm, err)
		}
		for _, s := range []string{"", "VmHWM: 1 kB\n", "VmHWM: -1 kB\nVmRSS: 2 kB\n", "VmHWM: 1 MB\nVmRSS: 2 kB\n", "VmHWM: 1 kB\nVmRSS: bad kB\n"} {
			if _, _, err := parseStatus(strings.NewReader(s)); err == nil {
				t.Errorf("accepted invalid status %q", s)
			}
		}
	})
	t.Run("samplerJoin", func(t *testing.T) {
		var baseline runtime.MemStats
		runtime.ReadMemStats(&baseline)
		stop := startSampler(baseline)
		result := stop()
		if result.count < 1 || result.peak.HeapAlloc < baseline.HeapAlloc || result.peak.HeapAlloc < result.endpoint.HeapAlloc {
			t.Fatalf("invalid samples: %+v", result)
		}
		if stop() != result {
			t.Fatal("repeated stop changed the joined result")
		}
	})
}
