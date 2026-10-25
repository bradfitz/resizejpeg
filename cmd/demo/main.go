// Copyright 2026 The resizejpeg Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command demo compares one JPEG's thumbnails with a full-decode Lanczos3
// reference. It is a research tool, not a resource-bounded image server.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/bradfitz/resizejpeg"
	xdraw "golang.org/x/image/draw"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "resizejpeg demo:", err)
		os.Exit(1)
	}
}

type demoReport struct {
	Input        string        `json:"input"`
	SourceWidth  int           `json:"sourceWidth"`
	SourceHeight int           `json:"sourceHeight"`
	Width        int           `json:"width"`
	Height       int           `json:"height"`
	Factor       int           `json:"thumbnailFactor"`
	Workers      int           `json:"workersRequested"`
	MaxPixels    uint64        `json:"maxSourcePixels"`
	Reference    string        `json:"reference"`
	Variants     []demoVariant `json:"variants"`
}

type demoVariant struct {
	Name     string  `json:"name"`
	Image    string  `json:"image"`
	Diff     string  `json:"diff"`
	MAE      float64 `json:"mae"`
	MaxError int     `json:"maxError"`
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("resizejpeg-demo", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "source JPEG (orientation and color profiles are not applied)")
	out := flags.String("out", "", "new report directory; must not already exist")
	width := flags.Int("width", 256, "target width; height preserves the original aspect ratio")
	workers := flags.Int("workers", 0, "decoder workers (0 = automatic, maximum 64)")
	maxPixels := flags.Uint64("max-pixels", 25_000_000, "maximum original pixels (0 = unlimited; trusted inputs only)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *input == "" || *out == "" {
		return errors.New("usage: go run ./cmd/demo -input photo.jpg -out /tmp/new-report [-width 256]")
	}
	if *width < 1 || *width > 65535 {
		return errors.New("width must be in [1, 65535]")
	}
	if *workers < 0 || *workers > 64 {
		return errors.New("workers must be in [0, 64]")
	}
	if _, err := os.Lstat(*out); err == nil {
		return fmt.Errorf("output directory already exists: %s", *out)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check output directory: %w", err)
	}
	data, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("JPEG header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		(*maxPixels != 0 && uint64(cfg.Width)*uint64(cfg.Height) > *maxPixels) {
		return fmt.Errorf("source dimensions %dx%d exceed the %d-pixel budget or are invalid", cfg.Width, cfg.Height, *maxPixels)
	}
	if *width > cfg.Width {
		return fmt.Errorf("target width %d exceeds source width %d; no upsampling", *width, cfg.Width)
	}
	height := max(1, int(int64(cfg.Height)*int64(*width)/int64(cfg.Width)))
	full, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("reference decode: %w", err)
	}
	fullRGB := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(fullRGB, fullRGB.Bounds(), full, full.Bounds().Min, draw.Src)
	reference := referenceResize(fullRGB, *width, height)
	opts := &resizejpeg.ResizeOptions{
		Workers: *workers, AllowFullDecode: true, MaxSourcePixels: *maxPixels,
	}
	thumbnail, err := resizejpeg.DecodeThumbnail(bytes.NewReader(data), *width, height, opts)
	if err != nil {
		return fmt.Errorf("thumbnail decode: %w", err)
	}
	streamed, err := resizejpeg.DecodeResize(bytes.NewReader(data), *width, height, opts)
	if err != nil {
		return fmt.Errorf("full-IDCT resize: %w", err)
	}
	report := demoReport{
		Input: filepath.Base(*input), SourceWidth: cfg.Width, SourceHeight: cfg.Height,
		Width: *width, Height: height, Factor: thumbnailFactor(cfg.Width, cfg.Height, *width, height, 4),
		Workers: *workers, MaxPixels: *maxPixels, Reference: "reference.png",
	}
	// Creating the directory atomically also catches a competing report writer.
	// Publish index.html last; failed runs never look like a complete report.
	if err := os.Mkdir(*out, 0755); err != nil {
		return fmt.Errorf("create fresh report directory: %w", err)
	}
	if err := writeDemoPNG(filepath.Join(*out, report.Reference), reference); err != nil {
		return err
	}
	thumbnailLabel := fmt.Sprintf("DecodeThumbnail: 4x headroom, IDCT 1/%d + Lanczos3", report.Factor)
	if cfg.Width < 4*(*width) || cfg.Height < 4*height {
		thumbnailLabel += " (source-limited headroom)"
	}
	for _, candidate := range []struct {
		name, filename string
		image          *image.RGBA
	}{
		{thumbnailLabel, "thumbnail", thumbnail},
		{"DecodeResize: full IDCT + Lanczos3", "full-idct", streamed},
	} {
		diff, mae, maxError := demoDifference(reference, candidate.image)
		variant := demoVariant{
			Name: candidate.name, Image: candidate.filename + ".png",
			Diff: candidate.filename + "-diff.png", MAE: mae, MaxError: maxError,
		}
		if err := writeDemoPNG(filepath.Join(*out, variant.Image), candidate.image); err != nil {
			return err
		}
		if err := writeDemoPNG(filepath.Join(*out, variant.Diff), diff); err != nil {
			return err
		}
		report.Variants = append(report.Variants, variant)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := writeDemoFile(filepath.Join(*out, "report.json"), func(w io.Writer) error {
		_, err := w.Write(append(encoded, '\n'))
		return err
	}); err != nil {
		return err
	}
	if err := writeDemoFile(filepath.Join(*out, "index.html"), func(w io.Writer) error {
		return demoHTML.Execute(w, report)
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Report: %s (%dx%d -> %dx%d)\n", filepath.Join(*out, "index.html"), cfg.Width, cfg.Height, *width, height)
	return nil
}

// thumbnailFactor independently applies the public headroom policy. It is
// used only for labels, not as a decoder diagnostic or a replacement for the
// library's own scale-selection tests. Compare before multiplying to avoid
// overflow with extreme headroom values.
func thumbnailFactor(sw, sh, width, height, headroom int) int {
	if sw <= 0 || sh <= 0 || width <= 0 || height <= 0 || headroom < 0 {
		return 1
	}
	if headroom == 0 {
		headroom = 4
	}
	goal := func(source, target int) uint64 {
		s, d, h := uint64(source), uint64(target), uint64(headroom)
		if d > s/h {
			return s
		}
		return d * h
	}
	w, h := goal(sw, width), goal(sh, height)
	for _, factor := range []uint64{8, 4, 2, 1} {
		if (uint64(sw)-1)/factor+1 >= w && (uint64(sh)-1)/factor+1 >= h {
			return int(factor)
		}
	}
	return 1
}

func referenceResize(src *image.RGBA, width, height int) *image.RGBA {
	kernel := &xdraw.Kernel{
		Support: 3,
		At: func(x float64) float64 {
			if x == 0 {
				return 1
			}
			x *= math.Pi
			return math.Sin(x) * math.Sin(x/3) / (x * x / 3)
		},
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	kernel.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// Difference images amplify absolute RGB errors eight times. The scalar
// metrics compare opaque 8-bit RGB and are not perceptual quality scores.
func demoDifference(reference, candidate *image.RGBA) (*image.RGBA, float64, int) {
	diff := image.NewRGBA(reference.Bounds())
	var sum int64
	maxError := 0
	for y := 0; y < reference.Rect.Dy(); y++ {
		for x := 0; x < reference.Rect.Dx(); x++ {
			ri, ci, di := reference.PixOffset(x, y), candidate.PixOffset(x, y), diff.PixOffset(x, y)
			for channel := 0; channel < 3; channel++ {
				delta := int(reference.Pix[ri+channel]) - int(candidate.Pix[ci+channel])
				if delta < 0 {
					delta = -delta
				}
				sum += int64(delta)
				maxError = max(maxError, delta)
				diff.Pix[di+channel] = byte(min(255, delta*8))
			}
			diff.Pix[di+3] = 255
		}
	}
	return diff, float64(sum) / float64(3*reference.Rect.Dx()*reference.Rect.Dy()), maxError
}

func writeDemoPNG(path string, im image.Image) error {
	return writeDemoFile(path, func(w io.Writer) error { return png.Encode(w, im) })
}

func writeDemoFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := write(f); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

var demoHTML = template.Must(template.New("demo").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>resizejpeg comparison</title>
<style>
body{font:16px system-ui,sans-serif;margin:2rem;color:#20242a;background:#f5f6f8}
main{max-width:1200px;margin:auto}.images{display:flex;gap:1rem;flex-wrap:wrap}
section{background:white;padding:1rem;border:1px solid #ccd1da;min-width:240px}
.image{overflow:auto;max-width:90vw}img{display:block;max-width:none}
h2{font-size:1rem}p{max-width:900px;line-height:1.5}a{color:#174e9e}
</style><main>
<h1>resizejpeg comparison</h1>
<p>{{.Input}}: {{.SourceWidth}}×{{.SourceHeight}} → {{.Width}}×{{.Height}}.
Original aspect ratio, rounded down; no cropping or upsampling.
Requested workers: {{.Workers}} (0 means automatic).</p>
<p>The reference fully decodes to opaque 8-bit RGB, then uses scale-aware Lanczos3
from x/image/draw. All filters operate in encoded RGB, not linear light.
No EXIF orientation or ICC profile is applied. Both resizing APIs explicitly
allow progressive/non-interleaved buffering; the reference always fully decodes.
The thumbnail factor is independently calculated from the documented policy,
not observed through a private decoder API.</p>
<div class="images"><section><h2>Full-decode Lanczos3 reference</h2>
<div class="image"><a href="{{.Reference}}"><img src="{{.Reference}}" alt="Reference"></a></div></section>
{{range .Variants}}<section><h2>{{.Name}}</h2>
<div class="image"><a href="{{.Image}}"><img src="{{.Image}}" alt="{{.Name}}"></a></div>
<p>RGB MAE {{printf "%.4f" .MAE}}; maximum error {{.MaxError}}.</p>
<p><a href="{{.Diff}}">Absolute difference, amplified 8×</a></p></section>{{end}}</div>
<p>Images display at natural size. Agreement with the reference is not an
absolute or perceptual quality score. <a href="report.json">Metrics JSON</a>.</p>
</main></html>`))
