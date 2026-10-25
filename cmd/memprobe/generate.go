// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
)

// The source has constant storage independent of both dimensions. Each palette
// entry is boxed once at construction, never in At's hot per-pixel path.
type probeImage struct {
	rect        image.Rectangle
	palette     [256]color.Color
	transparent color.Color
}

var _ image.Image = (*probeImage)(nil)

func newProbeImage(w, h int) *probeImage {
	m := &probeImage{rect: image.Rect(0, 0, w, h), transparent: color.NRGBA{}}
	for i := range m.palette {
		m.palette[i] = color.YCbCr{
			Y: uint8(32 + (i&15)*13), Cb: uint8(64 + ((i>>4)&3)*42), Cr: uint8(64 + ((i>>6)&3)*42),
		}
	}
	return m
}

func (m *probeImage) ColorModel() color.Model { return color.YCbCrModel }
func (m *probeImage) Bounds() image.Rectangle { return m.rect }
func (m *probeImage) Opaque() bool            { return true }
func (m *probeImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(m.rect) {
		return m.transparent
	}
	// Coherent colored structure plus deterministic fine texture, neither a
	// constant/striped low-entropy fixture nor a full-resolution noise buffer.
	ux, uy := uint32(x), uint32(y)
	noise := ux*0x1f123bb5 ^ uy*0x5f356495
	noise ^= noise >> 15
	noise *= 0x85ebca6b
	index := ((ux/3 + uy/5 + ((ux ^ uy) >> 2) + (ux/16+uy/16)*67) & 255) ^ ((noise >> 24) & 31)
	return m.palette[index]
}

func generate(path string, w, h int) (size int64, err error) {
	if err := dimensions(w, h); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	if err := jpeg.Encode(f, newProbeImage(w, h), &jpeg.Options{Quality: 80}); err != nil {
		return 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
