# Standalone peak-memory probe

`memprobe` is an opt-in research tool for trusted JPEGs, not a production
decoder example or an allocation benchmark. Build it once, generate inputs
separately, and use a **fresh process for each baseline or decode**:

```sh
CGO_ENABLED=0 go build -o /tmp/resizejpeg-memprobe ./cmd/memprobe

# Requires an explicit fresh path; refuses to overwrite an existing file.
GOMAXPROCS=2 /tmp/resizejpeg-memprobe -mode generate \
  -input /tmp/giant-20000x15000.jpg -size 20000x15000

GOMAXPROCS=2 GOGC=100 GOMEMLIMIT=off \
  /usr/bin/time -v /tmp/resizejpeg-memprobe -mode baseline

GOMAXPROCS=2 GOGC=100 GOMEMLIMIT=off \
  /usr/bin/time -v /tmp/resizejpeg-memprobe -mode decode \
  -input /tmp/giant-20000x15000.jpg -width 256 -workers 2
```

Generation defaults to 20000x15000, but never runs without `-mode generate`
and an explicit `-input` output path. It encodes a deterministic procedural
image at quality 80, with constant source-image storage and no full-size pixel
buffer. Generation time and memory do not belong in a decode measurement.
`-size WIDTHxHEIGHT` only determines generation dimensions.

Decode streams an `os.File`, reads configuration through a buffered reader,
then seeks and resets that reader before `DecodeThumbnail`. Target height is
`max(1, floor(sourceHeight*width/sourceWidth))`. Upsampling is rejected.
Workers default to 2; the filter and thumbnail headroom use library defaults.
The tool explicitly sets `AllowFullDecode: true` so progressive and supported
multi-scan fixtures can be measured. The source-pixel budget is deliberately
unlimited: these defaults are **not appropriate for untrusted input**.

`-max-memory 16777216` exercises the public decode-reservation budget. It is
not a heap or RSS cap. A decode failure writes a JSON measurement including
`error` to stdout, reports the error on stderr, and exits unsuccessfully.
Option/configuration errors report stderr without a measurement. Successful
operations write one JSON object to stdout. No `MemAllocSemaphore` is installed,
so the probe does not change decoder cleanup just to inspect reservations.
Private decoder fields and planned bytes are intentionally not reported.

## Measurement interpretation

Linux `/proc/self/status` supplies lifetime process high-water RSS (`VmHWM`)
and endpoint RSS (`VmRSS`), in KiB. Peaks include the Go runtime, probe,
configuration reading, sampling overhead, and decode; they are not
baseline-subtracted or incremental library memory. GNU `time` is a
cross-check of the kernel high-water metric, not an independent allocation
measurement. Repeat each case in fresh processes and report the largest
observed peak. Do not use `go run`, compilation, fixture generation, or
multiple in-process test invocations as a measured decode.

Go heap snapshots use bytes. The sampler runs every millisecond and takes an
endpoint sample before joining. `sampledPeak` contains the independent maximum
of each field, not a single simultaneous snapshot. Brief peaks can be missed;
`totalAlloc` is cumulative allocation, not peak live heap. `heapInuse`, `sys`,
and related heap statistics are not RSS. The tool does not change GC settings;
it reports the Go version, architecture, GOMAXPROCS, GOGC and GOMEMLIMIT
environment settings for reproducibility. Baseline mode takes only the
baseline snapshot; decode-only sample and endpoint fields remain zero.

**Historical difference:** the original tagged in-package harness retained a
private decoder reference through the endpoint heap/RSS reads, keeping all its
scratch buffers reachable. This standalone tool uses only the public API and
can retain only the returned thumbnail. Decoder scratch may become collectible
before the endpoint; endpoint and sampled heap numbers need not exactly match
the historical harness. There is no forced GC after decoding. Lifetime process
peak RSS is still observed in each fresh process, including memory used during
the decode.

Compressed input is never copied into a whole-file byte slice, and kernel file
page-cache contents are not process RSS. Runtime overhead, GC behavior,
concurrent work, and caller-retained data make these isolated results
inappropriate for predicting total server RSS or multiplying by concurrency.

Progressive fixtures may be prepared with `jpegtran` **outside** the measured
process:

```sh
jpegtran -maxmemory 2000000 -progressive \
  -outfile /tmp/giant-20000x15000-progressive.jpg \
  /tmp/giant-20000x15000.jpg
```

Then decode the resulting file in a fresh `memprobe` process. The tool itself
uses only Go code and the library; it never invokes a child process or native
decoder.

The generator and measurement helpers originated in the `jpegmemory` test
harness. This tool uses the BSD-style license in the repository root LICENSE.
