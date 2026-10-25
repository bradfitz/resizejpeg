# resizejpeg

Pure-Go JPEG decoding and resizing. No cgo, FFI, native libraries, or
subprocesses are used by the library. Its runtime dependencies are all in
the Go standard library.

```go
import "github.com/bradfitz/resizejpeg"

thumb, err := resizejpeg.DecodeThumbnail(r, 256, 192, &resizejpeg.ResizeOptions{
    MaxSourcePixels: 40_000_000,
    MaxMemoryBytes:  32 << 20,
})
```

The caller chooses exact output dimensions; the APIs do not automatically
fit aspect ratio, crop, upscale, apply EXIF orientation, or convert ICC
profiles. Progressive and non-interleaved inputs require explicit
`AllowFullDecode: true` permission for their image-wide buffering.

## API

- `DecodeThumbnail(r, width, height, opts)` selects reduced IDCT with
  four-times target headroom, then finishes with Lanczos3 by default.
- `DecodeResize(r, width, height, opts)` performs full IDCT and RGB clipping
  before filtering. Single-scan inputs do not allocate a full-size source
  image; bounded MCU-row bands pass through parallel workers.
- `Decode(r, factor)` decodes directly at full, half, quarter, or eighth
  resolution, using factors 1, 2, 4, or 8.
- `DecodeConfig(r)` reports original dimensions and color model.

The resizing APIs return `*image.RGBA`; reduced `Decode` returns an
`image.Image`. The package neither registers an `image` format nor changes
`image/jpeg` behavior.

### CPU, quality, and concurrency

`ResizeOptions.Filter` selects `Lanczos3` (zero/default), `CatmullRom`, or
`Bilinear`. `ThumbnailHeadroom` defaults to 4; choosing 2 can quarter the
intermediate pixel count when the available IDCT factors permit it.
It does not imply four-times less CPU or memory. `DecodeResize` honors the
filter but ignores positive thumbnail headroom.

`Workers: 0` uses `min(GOMAXPROCS, 8)` workers, further limited by source
bands. Entropy decoding remains serial; workers reconstruct, convert, and
horizontally filter bands, followed by ordered vertical accumulation.
The worker count is not a per-call CPU-core limit.

### Memory budgets

`MaxSourcePixels` bounds original pixel count. `MaxMemoryBytes` bounds a
conservative decode allocation plan, and `MemAllocSemaphore` allows one
shared, cancelable byte reservation before large allocations. The library
does not incrementally acquire reservations while holding earlier ones.

Returned pixels transfer to the caller. Limits are not hard heap or RSS
caps, and retained thumbnails, compressed inputs, encoders, caches, and GC
headroom need their own budgets. See [memory admission](docs/memory.md).

## Development and research

Go 1.27.2 or newer:

```sh
CGO_ENABLED=0 go test ./...
go test -race ./...
go vet ./...
```

Core tests include independent transform/filter oracles, upstream JPEG
regressions, malformed and progressive inputs, worker determinism, bounded
buffers, admission lifecycle, and fuzz targets. Tests use `x/image` as an
independent reference; it is not a library runtime dependency.

Optional tools and historical records live outside the library root:

- [`cmd/demo`](cmd/demo/README.md): single-image comparison command and the
  detailed interactive quality-report experiment. Native `djpeg` controls
  are optional and confined to the research tool.
- [`cmd/memprobe`](cmd/memprobe/README.md): lazy giant-fixture generation and
  fresh-process peak-memory measurements through the public API.
- [`docs/research`](docs/research/README.md): original measurements,
  limitations, and archived Perkeep-specific comparison harnesses.

## Upstream and license

The first commit imports `src/image/jpeg` and Go's `LICENSE` byte-for-byte
from Go commit **`022c8636110ebac86a9b88724cda168ed713c21f`**,
**October 8, 2026**, tag **`go1.27.2`**. It deliberately retains the encoder,
Go-internal imports, and original test paths as a pristine provenance
baseline, not a standalone build. The next commit makes a decoder-only
module; later commits add each resizing feature separately.

When updating Go, compare decoder, Huffman, scan, and inverse-DCT changes,
especially security fixes, against that upstream baseline.

All code uses Go's BSD-style [LICENSE](LICENSE). Upstream copyright notices
are retained.
