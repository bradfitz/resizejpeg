// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package resizejpeg decodes and resizes JPEG images without a full-size
// intermediate pixel buffer.
//
// It is a decoder-only fork of Go 1.27.2's image/jpeg package. [Decode] accepts
// a scale factor of 1, 2, 4, or 8. Reduced inverse DCTs produce smaller blocks
// directly, without allocating full-resolution pixels or decoding into an
// intermediate full-size image. The input is streamed, not buffered in full.
//
// Sequential JPEGs require only the reduced image buffers and block-sized
// scratch space. Progressive JPEGs additionally retain reduced-frequency
// coefficients and a nonzero bitmap for each source block. This bitmap is
// necessary to parse later AC refinement scans correctly; its memory usage
// depends on the source dimensions, not just the output dimensions.
//
// [DecodeResize] instead performs full IDCT and RGB conversion before applying
// a selectable scale-aware filter (Lanczos3 by default). Single-scan inputs use
// bounded MCU-row buffers and parallel workers, rather than a full-resolution
// source image. Progressive
// and non-interleaved inputs require an explicit full-buffer fallback; see
// [ResizeOptions].
//
// [DecodeThumbnail] instead selects reduced IDCT with configurable thumbnail
// headroom, then finishes with the selected filter. Defaults are four-times
// headroom and Lanczos3; CatmullRom and Bilinear use narrower kernels.
// [ResizeOptions.ThumbnailHeadroom] can reduce intermediate pixel work
// independently of [ResizeOptions.Filter].
// The same workers perform reduced transforms and horizontal filtering.
// Original-image sampling coordinates avoid rounded-size phase drift.
// This is faster but not pixel-equivalent to full decoding plus Lanczos3.
//
// JPEG is defined in ITU-T T.81: https://www.w3.org/Graphics/JPEG/itu-t81.pdf.
package resizejpeg // import "github.com/bradfitz/resizejpeg"
