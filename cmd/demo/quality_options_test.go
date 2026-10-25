//go:build jpegquality

// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"image"
	"image/color"
	"image/draw"
	"os/exec"
	"strings"
	"testing"

	"github.com/bradfitz/resizejpeg"
)

func qualityOptionsJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			src.SetRGBA(x, y, color.RGBA{
				byte(7*x + 3*y), byte(2*x*x + y), byte(x + 5*y*y), 255,
			})
		}
	}
	return resizeStreamEncode(t, src)
}

func TestQualityOptionsReferences(t *testing.T) {
	for _, filter := range []resizejpeg.Filter{resizejpeg.Lanczos3, resizejpeg.CatmullRom, resizejpeg.Bilinear} {
		t.Run(qualityFilterName(filter), func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, 31, 29))
			want := color.RGBA{17, 127, 239, 255}
			draw.Draw(src, src.Rect, image.NewUniform(want), image.Point{}, draw.Src)
			got := qualityResizeFilter(src, 7, 5, filter)
			for y := 0; y < 5; y++ {
				for x := 0; x < 7; x++ {
					if c := got.RGBAAt(x, y); c != want {
						t.Fatalf("constant changed at %d,%d: got %v, want %v", x, y, c, want)
					}
				}
			}
		})
	}
}

func TestQualityOptionsCandidateLabels(t *testing.T) {
	data := qualityOptionsJPEG(t, 129, 97)
	for _, tc := range []struct {
		filter   resizejpeg.Filter
		headroom int
		w, h     int
		want     string
	}{
		{resizejpeg.Lanczos3, 0, 16, 12, "Pure Go 4x-headroom IDCT 1/2 + Lanczos3"},
		{resizejpeg.Lanczos3, 2, 16, 12, "Pure Go 2x-headroom IDCT 1/4 + Lanczos3"},
		{resizejpeg.CatmullRom, 4, 16, 12, "Pure Go 4x-headroom IDCT 1/2 + CatmullRom"},
		{resizejpeg.Bilinear, 4, 16, 12, "Pure Go 4x-headroom IDCT 1/2 + Bilinear"},
		{resizejpeg.Bilinear, 2, 16, 12, "Pure Go 2x-headroom IDCT 1/4 + Bilinear"},
		{resizejpeg.Bilinear, 4, 64, 48, "Pure Go 4x-headroom IDCT 1/1 + Bilinear (source-limited headroom)"},
		{resizejpeg.Bilinear, 2, 1, 1, "Pure Go 2x-headroom IDCT 1/8 + Bilinear"},
	} {
		got := qualityThumbnailCandidate(t, data, 129, 97, tc.w, tc.h, tc.filter, tc.headroom)
		if got.name != tc.want {
			t.Errorf("filter=%v headroom=%d size=%dx%d: name %q, want %q", tc.filter, tc.headroom, tc.w, tc.h, got.name, tc.want)
		}
		if got.im.Bounds() != image.Rect(0, 0, tc.w, tc.h) {
			t.Fatal("candidate returned incorrect dimensions")
		}
	}
}

func TestQualityOptionsProgressive(t *testing.T) {
	jpegtran, err := exec.LookPath("jpegtran")
	if err != nil {
		t.Skip("jpegtran unavailable: options baseline/progressive checks NOT run")
	}
	if checks := qualityCheckProgressive(t, jpegtran, qualityOptionsJPEG(t, 129, 97)); checks != 10 {
		t.Fatalf("got %d progressive checks, want 10", checks)
	}
	// Keep this control at a genuine thumbnail size too: the tiny odd fixture
	// above is source-limited at the checker's 256-pixel maximum long edge.
	data := qualityOptionsJPEG(t, 1024, 768)
	if checks := qualityCheckProgressive(t, jpegtran, data); checks != 10 {
		t.Fatalf("got %d progressive checks, want 10", checks)
	}
	candidate := qualityThumbnailCandidate(t, data, 1024, 768, 256, 192, resizejpeg.Bilinear, 2)
	if !strings.Contains(candidate.name, "IDCT 1/2") {
		t.Fatalf("nondefault thumbnail did not reduce: %q", candidate.name)
	}
}
