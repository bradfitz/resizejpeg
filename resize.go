// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resizejpeg

import (
	"image"
	"image/color"
	"image/draw"
	"io"
	"runtime"
	"sync"
)

// ResizeOptions controls [DecodeResize]. A nil pointer uses the zero values.
type ResizeOptions struct {
	// Workers is the number of full-IDCT/RGB/horizontal-filter workers.
	// Zero uses min(GOMAXPROCS, 8); explicit values must be in [1, 64].
	// The number is further limited by the number of source MCU rows.
	// Entropy decoding and ordered vertical filtering remain serial.
	Workers int

	// AllowFullDecode permits a full-resolution fallback for progressive
	// images and sequential scans that do not contain all components.
	// By default those inputs return UnsupportedError before allocating
	// source image planes or progressive coefficient arrays.
	//
	// The fallback uses image-wide pixel/coefficient buffers. It does not
	// preserve the streaming path's bounded-scratch-memory guarantee.
	AllowFullDecode bool

	// MaxSourcePixels limits width*height before allocating source buffers.
	// Zero imposes no pixel budget. This is not a total memory/CPU limit:
	// callers should also bound concurrent requests and compressed input.
	MaxSourcePixels uint64
}

// DecodeResize fully decodes JPEG samples and resizes them with scale-aware
// Lanczos3, returning an opaque RGB image of exactly width by height pixels.
// Both dimensions must be positive and no larger than the source dimensions.
// Non-positive or out-of-JPEG-range target dimensions and invalid worker
// counts are rejected without reading r. Upsampling is rejected after reading
// the source dimensions.
//
// Unlike [Decode] at reduced factors, this applies full 8x8 IDCT, sample
// clipping, and RGB conversion before resizing. Filtering uses encoded RGB
// values, not linear light; ICC profiles and EXIF orientation are not applied.
//
// Single-scan grayscale and interleaved color images use bounded MCU-row
// buffers rather than a full-resolution source image. A bounded worker pool
// overlaps entropy decoding with IDCT, RGB conversion, and horizontal
// filtering; an ordered collector performs vertical filtering. Scratch memory
// depends on source width, destination dimensions, and worker count, not on
// source height. The destination image itself is always allocated in full.
//
// See [ResizeOptions.AllowFullDecode] for progressive and non-interleaved
// inputs. Additional scans after an already streamed scan, or color metadata
// changing after that scan, are unsupported even when fallback is allowed:
// streaming cannot replay previously discarded source data.
func DecodeResize(r io.Reader, width, height int, opts *ResizeOptions) (*image.RGBA, error) {
	im, _, err := decodeResize(r, width, height, opts)
	return im, err
}

// Returning the decoder lets package tests inspect the actual owned buffers
// and verify worker shutdown, rather than inferring memory use from output size.
func decodeResize(r io.Reader, width, height int, opts *ResizeOptions) (*image.RGBA, *decoder, error) {
	var o ResizeOptions
	if opts != nil {
		o = *opts
	}
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return nil, nil, UnsupportedError("resize dimensions must be in [1, 65535]")
	}
	if o.Workers < 0 || o.Workers > 64 {
		return nil, nil, UnsupportedError("resize workers must be in [0, 64]")
	}
	if o.Workers == 0 {
		o.Workers = min(runtime.GOMAXPROCS(0), 8)
	}
	req := &resizeRequest{width: width, height: height, opts: o}
	d := &decoder{factor: 1, resize: req}
	im, err := d.decode(r, false)
	if err != nil {
		return nil, d, err
	}
	if req.streaming {
		return req.filter.dst, d, nil
	}
	// Only explicitly permitted inputs can get here with a full source image.
	f, err := newLanczosResizer(d.width, d.height, width, height)
	if err != nil {
		return nil, d, err
	}
	req.filter = f
	row := image.NewRGBA(image.Rect(0, 0, d.width, 1))
	values := make([]float64, 3*width)
	for y := 0; y < d.height; y++ {
		draw.Draw(row, row.Rect, im, image.Pt(0, y), draw.Src)
		f.horizontal(values, row.Pix)
		if err := f.addRows(values, y, 1); err != nil {
			return nil, d, err
		}
	}
	if err := f.finish(); err != nil {
		return nil, d, err
	}
	return f.dst, d, nil
}

type resizeRequest struct {
	width, height int
	opts          ResizeOptions
	streaming     bool
	fallback      bool
	complete      bool
	scans         int
	filter        *lanczosResizer
	pipeline      *resizePipeline
	rgb           bool
	adobeValid    bool
	adobe         uint8
}

func (r *resizeRequest) source(d *decoder) error {
	if r.width > d.width || r.height > d.height {
		return UnsupportedError("resize target exceeds source dimensions")
	}
	if r.opts.MaxSourcePixels != 0 && uint64(d.width)*uint64(d.height) > r.opts.MaxSourcePixels {
		return UnsupportedError("resize source exceeds pixel budget")
	}
	if d.progressive && !r.opts.AllowFullDecode {
		return UnsupportedError("progressive resize requires AllowFullDecode")
	}
	return nil
}

func (r *resizeRequest) startScan(d *decoder, nComp, mxx, myy int) error {
	r.scans++
	if r.streaming {
		return UnsupportedError("additional scan after streaming resize")
	}
	if r.fallback {
		return nil
	}
	if d.progressive || nComp != d.nComp || d.nComp == 4 && !d.adobeTransformValid {
		if !r.opts.AllowFullDecode {
			return UnsupportedError("streaming resize requires one complete interleaved scan and known color model")
		}
		r.fallback = true
		return nil
	}
	if d.nComp == 3 && d.isRGB() {
		h0, h1, h2 := d.comp[0].h, d.comp[1].h, d.comp[2].h
		v0, v1, v2 := d.comp[0].v, d.comp[1].v, d.comp[2].v
		if h1 != h2 || h0%h1 != 0 || v1 != v2 || v0%v1 != 0 {
			return errUnsupportedSubsamplingRatio
		}
	}
	f, err := newLanczosResizer(d.width, d.height, r.width, r.height)
	if err != nil {
		return err
	}
	r.filter, r.rgb = f, d.isRGB()
	r.adobeValid, r.adobe = d.adobeTransformValid, d.adobeTransform
	p, err := newResizePipeline(d, r, mxx, myy)
	if err != nil {
		return err
	}
	r.pipeline, r.streaming = p, true
	return nil
}

func (r *resizeRequest) result(d *decoder) (image.Image, error) {
	if !r.complete {
		return nil, FormatError("incomplete streamed scan")
	}
	if r.rgb != d.isRGB() || r.adobeValid != d.adobeTransformValid || r.adobe != d.adobeTransform {
		return nil, UnsupportedError("color metadata changed after streamed scan")
	}
	return r.filter.dst, nil
}

type resizeBand struct {
	y, n   int
	coeffs [maxComponents][]block
	rows   []float64 // Packed, horizontally filtered RGB rows.
	err    error
}

type resizeWorker struct {
	d    decoder // Only one MCU row's image planes; no entropy state in use.
	rgba *image.RGBA
}

type resizePipeline struct {
	filter   *lanczosResizer
	bands    []*resizeBand
	workers  []*resizeWorker
	free     chan *resizeBand
	jobs     chan *resizeBand
	results  chan *resizeBand
	errCh    chan error
	done     chan struct{}
	wg       sync.WaitGroup
	joined   bool
	firstErr error // Collector-owned until done closes.
}

func newResizePipeline(d *decoder, req *resizeRequest, mxx, myy int) (*resizePipeline, error) {
	nw := min(req.opts.Workers, myy)
	nb := min(nw+1, myy)
	bandHeight := 8 * d.maxV
	p := &resizePipeline{
		filter: req.filter, free: make(chan *resizeBand, nb),
		jobs: make(chan *resizeBand, nw), results: make(chan *resizeBand, nw),
		errCh: make(chan error, 1), done: make(chan struct{}),
	}
	rowValues := uint64(bandHeight) * uint64(req.width) * 3
	if err := checkAllocationSize(rowValues * 8 * uint64(nb)); err != nil {
		return nil, err
	}
	for ci := 0; ci < d.nComp; ci++ {
		count := uint64(mxx) * uint64(d.comp[ci].h*d.comp[ci].v)
		if err := checkAllocationSize(count * 256 * uint64(nb)); err != nil {
			return nil, err
		}
	}
	for range nb {
		b := &resizeBand{rows: make([]float64, int(rowValues))}
		for ci := 0; ci < d.nComp; ci++ {
			b.coeffs[ci] = make([]block, mxx*d.comp[ci].h*d.comp[ci].v)
		}
		p.bands = append(p.bands, b)
		p.free <- b
	}
	for range nw {
		w := &resizeWorker{d: decoder{
			width: d.width, height: bandHeight, factor: 1, nComp: d.nComp,
			maxH: d.maxH, maxV: d.maxV, comp: d.comp, quant: d.quant,
			jfif: d.jfif, adobeTransformValid: d.adobeTransformValid, adobeTransform: d.adobeTransform,
		}}
		if err := w.d.makeImg(mxx, 1); err != nil {
			return nil, err
		}
		if err := checkAllocationSize(uint64(d.width) * uint64(bandHeight) * 4); err != nil {
			return nil, err
		}
		w.rgba = image.NewRGBA(image.Rect(0, 0, d.width, bandHeight))
		p.workers = append(p.workers, w)
	}
	// No goroutines start until every allocation/validation above succeeds.
	go p.collect()
	for _, w := range p.workers {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for b := range p.jobs {
				b.err = w.process(b, p.filter, mxx)
				p.results <- b
			}
		}()
	}
	return p, nil
}

func (p *resizePipeline) acquire() (*resizeBand, error) {
	select {
	case err := <-p.errCh:
		return nil, err
	case b := <-p.free:
		b.err = nil
		return b, nil
	}
}

func (p *resizePipeline) submit(b *resizeBand) error {
	select {
	case err := <-p.errCh:
		return err
	case p.jobs <- b:
		return nil
	}
}

func (p *resizePipeline) collect() {
	defer close(p.done)
	pending := make(map[int]*resizeBand, len(p.bands))
	next := 0
	recordError := func(err error) {
		if p.firstErr == nil && err != nil {
			p.firstErr = err
			p.errCh <- err
		}
	}
	for b := range p.results {
		recordError(b.err)
		pending[b.y] = b
		// A slot is recycled only after ordered consumption. Thus a slow
		// early band cannot permit an unbounded queue of later results.
		for {
			b := pending[next]
			if b == nil {
				break
			}
			delete(pending, next)
			if p.firstErr == nil {
				recordError(p.filter.addRows(b.rows, b.y, b.n))
			}
			next += b.n
			p.free <- b
		}
	}
}

func (p *resizePipeline) closeAndWait() error {
	close(p.jobs)
	p.wg.Wait()
	close(p.results)
	<-p.done
	p.joined = true
	return p.firstErr
}

func (w *resizeWorker) process(b *resizeBand, f *lanczosResizer, mxx int) error {
	d := &w.d
	for ci := 0; ci < d.nComp; ci++ {
		stride := mxx * d.comp[ci].h
		for by := 0; by < d.comp[ci].v; by++ {
			for bx := 0; bx < stride; bx++ {
				if err := d.reconstructBlock(&b.coeffs[ci][by*stride+bx], bx, by, ci); err != nil {
					return err
				}
			}
		}
	}
	rect := image.Rect(0, 0, d.width, b.n)
	if d.nComp == 1 {
		draw.Draw(w.rgba, rect, d.img1, image.Point{}, draw.Src)
	} else if d.nComp == 3 && !d.isRGB() {
		draw.Draw(w.rgba, rect, d.img3, image.Point{}, draw.Src)
	} else if d.nComp == 3 {
		scale := d.comp[0].h / d.comp[1].h
		for y := 0; y < b.n; y++ {
			yo, co := d.img3.YOffset(0, y), d.img3.COffset(0, y)
			dst := w.rgba.Pix[y*w.rgba.Stride:]
			for x := 0; x < d.width; x++ {
				dst[4*x+0], dst[4*x+1], dst[4*x+2], dst[4*x+3] =
					d.img3.Y[yo+x], d.img3.Cb[co+x/scale], d.img3.Cr[co+x/scale], 255
			}
		}
	} else {
		if d.adobeTransform != adobeTransformUnknown {
			draw.Draw(w.rgba, rect, d.img3, image.Point{}, draw.Src)
		}
		for y := 0; y < b.n; y++ {
			yo := d.img3.YOffset(0, y)
			dst := w.rgba.Pix[y*w.rgba.Stride:]
			for x := 0; x < d.width; x++ {
				k := 255 - d.blackPix[y*d.blackStride+x]
				var c, m, yellow byte
				if d.adobeTransform == adobeTransformUnknown {
					// The original CMYK decode inverts Adobe's samples.
					c = 255 - d.img3.Y[yo+x]
					m = 255 - d.img3.Cb[d.img3.COffset(x, y)]
					yellow = 255 - d.img3.Cr[d.img3.COffset(x, y)]
				} else {
					c, m, yellow = dst[4*x], dst[4*x+1], dst[4*x+2]
				}
				dst[4*x], dst[4*x+1], dst[4*x+2] = color.CMYKToRGB(c, m, yellow, k)
				dst[4*x+3] = 255
			}
		}
	}
	stride := 3 * f.dst.Rect.Dx()
	for y := 0; y < b.n; y++ {
		f.horizontal(b.rows[y*stride:(y+1)*stride], w.rgba.Pix[y*w.rgba.Stride:])
	}
	return nil
}
