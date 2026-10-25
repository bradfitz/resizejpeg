// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjpeg

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestReadPNM(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  color.Color
	}{
		{"P5\n1 1\n255\n\x7f", color.Gray{Y: 127}},
		{"P6\n1 1\n255\n\x10\x20\x30", color.RGBA{16, 32, 48, 255}},
	} {
		im, err := readPNM(bytes.NewBufferString(tc.input))
		if err != nil {
			t.Fatal(err)
		}
		if im.Bounds() != image.Rect(0, 0, 1, 1) || im.At(0, 0) != tc.want {
			t.Fatalf("got %v %v, want 1x1 %v", im.Bounds(), im.At(0, 0), tc.want)
		}
	}
	for _, input := range []string{"", "P4\n1 1\n255\n\x00", "P5\n1 1\n255\n", "P6\n1 1\n255\n\x00"} {
		if _, err := readPNM(bytes.NewBufferString(input)); err == nil {
			t.Errorf("accepted invalid PNM %q", input)
		}
	}
}

func TestFactor(t *testing.T) {
	for _, tc := range []struct{ w, h, sw, sh, want int }{
		{4000, 3000, 256, 192, 8},
		{257, 193, 32, 24, 8},
		{257, 193, 33, 25, 4},
		{129, 97, 64, 48, 2},
		{129, 97, 65, 49, 1},
	} {
		if got := Factor(tc.w, tc.h, tc.sw, tc.sh); got != tc.want {
			t.Errorf("%+v: got %d", tc, got)
		}
	}
}

func TestDjpeg(t *testing.T) {
	if !Available() {
		t.Skip("djpeg unavailable or disabled: native decoder NOT covered")
	}
	for _, gray := range []bool{false, true} {
		var src image.Image = image.NewRGBA(image.Rect(0, 0, 129, 97))
		if gray {
			src = image.NewGray(src.Bounds())
		}
		var data bytes.Buffer
		if err := jpeg.Encode(&data, src, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		for _, factor := range []int{1, 2, 4, 8} {
			t.Run(fmt.Sprintf("gray=%t/factor=%d", gray, factor), func(t *testing.T) {
				im, err := DecodeDownsample(bytes.NewReader(data.Bytes()), factor)
				if err != nil {
					t.Fatal(err)
				}
				want := image.Rect(0, 0, (129+factor-1)/factor, (97+factor-1)/factor)
				if im.Bounds() != want {
					t.Fatalf("bounds %v, want %v", im.Bounds(), want)
				}
			})
		}
	}
}
