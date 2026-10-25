# resizejpeg

[![Go Reference](https://pkg.go.dev/badge/github.com/bradfitz/resizejpeg.svg)](https://pkg.go.dev/github.com/bradfitz/resizejpeg)

An AI-edited version of Go's standard `image/jpeg` package, developed for
[Perkeep](https://github.com/perkeep/perkeep)'s JPEG thumbnailing needs.

The library is pure Go, with only standard-library runtime dependencies.
No cgo, FFI, native libraries, or subprocesses.

## What it adds over `image/jpeg`

- **Reduced-IDCT decoding:** reconstruct smaller images directly instead of
  allocating full-resolution pixels. Single-scan thumbnail generation
  streams through bounded row buffers.
- **Multiple CPU cores:** parallelize inverse transforms, color conversion,
  and horizontal filtering. Entropy decoding remains serial.
- **Integrated resampling:** optionally finish decoding with Lanczos3,
  Catmull–Rom, or bilinear filtering, with configurable decode headroom for
  different CPU-versus-quality trade-offs.
- **Caller-controlled memory admission:** per-decode allocation budgets and
  a byte-weighted reservation hook let callers supply their own policy for
  bounding concurrent decode memory, waiting for capacity, and canceling
  admission waits.

Progressive and non-interleaved inputs still require image-wide state.
Memory budgets cover planned decoder allocations, not hard process RSS
limits or caller-retained thumbnails. See [memory admission](docs/memory.md)
for the policy and ownership details.

## Tools and research

- [`cmd/demo`](cmd/demo/README.md): single-image comparison command and the
  detailed interactive quality-report experiment. Native `djpeg` controls
  are optional and confined to the research tool.
- [`cmd/memprobe`](cmd/memprobe/README.md): lazy giant-fixture generation and
  fresh-process peak-memory measurements through the public API.
- [`docs/research`](docs/research/README.md): original measurements,
  limitations, and archived Perkeep-specific comparison harnesses.

## Upstream and license

The history starts with an unchanged import of Go's `image/jpeg` and
license; subsequent commits contain the edits. All code uses Go's
BSD-style [LICENSE](LICENSE), with upstream copyright notices retained.
