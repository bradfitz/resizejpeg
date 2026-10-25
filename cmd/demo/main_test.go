// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThumbnailFactor(t *testing.T) {
	for _, tc := range []struct {
		sw, sh, width, height, headroom, want int
	}{
		{129, 97, 16, 12, 0, 2},
		{129, 97, 16, 12, 2, 4},
		{129, 97, 16, 12, 4, 2},
		{129, 97, 64, 48, 4, 1},
		{129, 97, 1, 1, 2, 8},
		{4000, 3000, 256, 192, 4, 2},
		{3, 5, 1, 1, int(^uint(0) >> 1), 1},
		{0, 5, 1, 1, 4, 1},
		{3, 5, 1, 1, -1, 1},
	} {
		if got := thumbnailFactor(tc.sw, tc.sh, tc.width, tc.height, tc.headroom); got != tc.want {
			t.Errorf("%+v: got %d", tc, got)
		}
	}
}

func demoTestJPEG(t *testing.T, width, height int) string {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetRGBA(x, y, color.RGBA{byte(7*x + y), byte(3*y + x*x), byte(5*x + 2*y*y), 255})
		}
	}
	path := filepath.Join(t.TempDir(), "input.jpg")
	if err := os.WriteFile(path, resizeStreamEncode(t, src), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDemoReport(t *testing.T) {
	for _, size := range []image.Point{{129, 97}, {65, 129}} {
		t.Run(size.String(), func(t *testing.T) {
			input := demoTestJPEG(t, size.X, size.Y)
			out := filepath.Join(t.TempDir(), "report")
			args := []string{"-input", input, "-out", out, "-width", "16", "-workers", "1"}
			var stdout bytes.Buffer
			if err := run(args, &stdout, io.Discard); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "index.html") {
				t.Fatalf("missing report path in %q", stdout.String())
			}
			data, err := os.ReadFile(filepath.Join(out, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report demoReport
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			want := image.Pt(16, max(1, size.Y*16/size.X))
			if report.SourceWidth != size.X || report.SourceHeight != size.Y ||
				report.Width != want.X || report.Height != want.Y || len(report.Variants) != 2 {
				t.Fatalf("incorrect report: %+v", report)
			}
			names := []string{report.Reference}
			for _, variant := range report.Variants {
				names = append(names, variant.Image, variant.Diff)
			}
			for _, name := range names {
				f, err := os.Open(filepath.Join(out, name))
				if err != nil {
					t.Fatal(err)
				}
				im, err := png.Decode(f)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				if im.Bounds().Size() != want {
					t.Errorf("%s: size %v, want %v", name, im.Bounds().Size(), want)
				}
			}
			if report.Variants[1].MaxError > 1 {
				t.Fatalf("full-IDCT output differs from reference by more than one level: %+v", report.Variants[1])
			}
			before, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if err := run(args, io.Discard, io.Discard); err == nil {
				t.Fatal("reused a report directory")
			}
			after, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("overwrote an existing report")
			}
		})
	}
}

func TestDemoRejectsInvalidInput(t *testing.T) {
	input := demoTestJPEG(t, 33, 25)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"upscale", []string{"-width", "34"}},
		{"pixel-budget", []string{"-width", "16", "-max-pixels", "100"}},
		{"zero-width", []string{"-width", "0"}},
		{"workers", []string{"-workers", "65"}},
		{"extra-argument", []string{"extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "report")
			args := append([]string{"-input", input, "-out", out}, tc.args...)
			if err := run(args, io.Discard, io.Discard); err == nil {
				t.Fatal("accepted invalid input")
			}
			if _, err := os.Lstat(out); !os.IsNotExist(err) {
				t.Fatalf("created output before validation: %v", err)
			}
		})
	}
	out := filepath.Join(t.TempDir(), "report")
	if err := run([]string{"-input", filepath.Join(t.TempDir(), "missing"), "-out", out}, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted a missing input")
	}
	if err := run(nil, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted missing arguments")
	}
}

func TestDemoDifference(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 1, 1)), image.NewRGBA(image.Rect(0, 0, 1, 1))
	a.SetRGBA(0, 0, color.RGBA{10, 20, 30, 255})
	b.SetRGBA(0, 0, color.RGBA{9, 22, 27, 255})
	diff, mae, maximum := demoDifference(a, b)
	if mae != 2 || maximum != 3 || diff.RGBAAt(0, 0) != (color.RGBA{8, 16, 24, 255}) {
		t.Fatalf("difference %v, MAE %v, max %d", diff.RGBAAt(0, 0), mae, maximum)
	}
}
