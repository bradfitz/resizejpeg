# Research records

These records preserve the development experiments originally performed in
Perkeep draft PR #1949. They are intentionally separate from library usage
and API documentation. Original development code was migrated from Perkeep
commit `9f264edeec89dfbd3ee26c76e639389a259cf957`.

## Current tools

From this repository's root:

```sh
# Public-API comparison of one original JPEG; output directory must be new.
go run ./cmd/demo -input photo.jpg -out /tmp/resizejpeg-demo -width 256

# Detailed report helper tests; report generation remains opt-in.
CGO_ENABLED=0 go test -tags=jpegquality ./cmd/demo -count=1

# Pure-Go filter/headroom benchmark; fix the CPU limit in a fresh process.
GOMAXPROCS=2 CGO_ENABLED=0 go test . -run '^$' \
  -bench '^BenchmarkDecodeThumbnailOptions$' -benchmem -cpu=2 -count=5

# Compile once, then measure the public API in a fresh process per decode.
CGO_ENABLED=0 go build -o /tmp/resizejpeg-memprobe ./cmd/memprobe
```

See the [demo instructions](../../cmd/demo/README.md) for the pinned
public-domain photo downloads and detailed report generation. See the
[memory probe](../../cmd/memprobe/README.md) for fixture generation,
progressive conversion, Linux RSS, GC settings, and byte-budget probes.

The standalone tools use public APIs and cannot inspect private decoder
state. In particular, the new memory probe does not retain the private
decoder at its endpoint; historical endpoint heaps are not directly
comparable. Its fresh-process peak RSS still includes the decode itself.
No private diagnostic API was added just to support research tools.

## Historical findings

- [Initial reduced-IDCT investigation](INITIAL.md).
- [Full-IDCT streaming architecture and measurements](STREAMING.md).
- [Headroom decoding, filter options, quality and performance](THUMBNAIL.md).
- [Visual comparisons and stress-case limitations](QUALITY.md).
- [Giant-input peak memory and admission checks](PEAKMEMORY.md).
- [Comparison against Perkeep's old native thumbnail pipeline](LEGACY.md).

The records retain their original-checkout commands and measurements;
paths such as `internal/images/resizejpeg` and benchmark names prefixed
`legacy-production` refer to the old Perkeep checkout, not this module.
Do not describe the standalone native decoder + Lanczos control as the
whole Perkeep production thumbnail path. They perform different work.

The Perkeep-specific [comparison benchmark](perkeep/benchresizejpeg_test.go.txt)
and [original in-package memory probe](perkeep/memory_test.go.txt) are
archived as noncompiled source for provenance and reproducibility of the
old numbers. License headers are normalized to this repository's BSD
license. The benchmark requires its original surrounding Perkeep code;
these files are not extra public APIs or module dependencies.

The abandoned TurboJPEG/purego FFI experiment was not migrated. Neither
this module nor its tools require `github.com/ebitengine/purego` or Perkeep.
The optional native comparison adapter in `cmd/demo/internal/fastjpeg` is
only a subprocess-based historical control, never part of the library.

Quality errors are comparisons with a chosen full-decode reference, not a
guarantee over all photographs or synthetic patterns. Reduced IDCT can
alias periodic detail or differ on saturated colors; default headroom
helped the tested photographs but is not pixel-equivalent to full IDCT.
