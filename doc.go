// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package resizejpeg decodes JPEG images directly at reduced resolution.
//
// It is a decoder-only fork of Go 1.27.2's image/jpeg package. Decode accepts
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
// JPEG is defined in ITU-T T.81: https://www.w3.org/Graphics/JPEG/itu-t81.pdf.
package resizejpeg // import "github.com/bradfitz/resizejpeg"
