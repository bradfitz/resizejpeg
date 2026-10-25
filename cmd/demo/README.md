# JPEG comparison tools

These optional tools use the public `resizejpeg` API. They do not import
Perkeep, use cgo or FFI, or add native dependencies to the library.

## One JPEG

From the repository root:

```sh
go run ./cmd/demo -input photo.jpg -out /tmp/resizejpeg-demo -width 256
```

The output directory must be **new**, and its parent must already exist.
Open its `index.html` to compare:

- Standard-library full decode, opaque 8-bit RGB, then scale-aware Lanczos3
  using the independent `golang.org/x/image/draw` implementation.
- `DecodeThumbnail` with default four-times headroom and Lanczos3.
- `DecodeResize` with full IDCT and Lanczos3.

The report includes lossless PNGs, amplified differences, and `report.json`.
Height preserves the original aspect ratio, rounded down to at least one
pixel. No cropping, upsampling, EXIF orientation, or ICC conversion is done.
The default original-image limit is 25 megapixels, checked before decoding
pixels. Override with `-max-pixels`; zero removes this limit. `-workers`
defaults to zero (automatic).

This is a trusted-input comparison program, **not a memory-bounded server**:
it reads the compressed file and fully decodes the reference, and explicitly
allows progressive/non-interleaved buffering in both resizing APIs.
Factor labels use an independent implementation of the documented headroom
policy; no private decoder diagnostics are exposed.

## Detailed historical quality report

The larger opt-in experiment preserves the original patterns, quality-95
encoding, reference kernels, candidate order, and pixel comparisons. It also
checks baseline/progressive parity when `jpegtran` is installed. It uses
`x/image`'s font packages for synthetic text patterns.

Run its helper tests without generating a report:

```sh
CGO_ENABLED=0 go test -tags=jpegquality ./cmd/demo -run '^TestQuality' -count=1
```

Generate a synthetic-only report into a **fresh** directory:

```sh
RESIZEJPEG_QUALITY_DIR=/tmp/resizejpeg-quality \
CGO_ENABLED=0 go test -tags=jpegquality ./cmd/demo \
  -run '^TestQualityReport$' -count=1 -v
```

To include the original public-domain photographs, download and verify the
manifest's pinned files separately. This command uses the source URLs and
credits recorded in `testdata/quality-photos.json`:

```sh
mkdir -p /tmp/resizejpeg-photos
cp cmd/demo/testdata/quality-photos.json /tmp/resizejpeg-photos/photos.json
python3 - /tmp/resizejpeg-photos/photos.json <<'PY'
import hashlib, json, pathlib, sys, urllib.request
manifest = pathlib.Path(sys.argv[1])
for photo in json.loads(manifest.read_text()):
    data = urllib.request.urlopen(photo["download"]).read()
    assert hashlib.sha256(data).hexdigest() == photo["sha256"], photo["name"]
    (manifest.parent / photo["path"]).write_bytes(data)
PY
RESIZEJPEG_QUALITY_DIR=/tmp/resizejpeg-quality-photos \
RESIZEJPEG_QUALITY_PHOTOS=/tmp/resizejpeg-photos/photos.json \
CGO_ENABLED=0 go test -tags=jpegquality ./cmd/demo \
  -run '^TestQualityReport$' -count=1 -v
```

If the browser disallows loading local `report.json`, serve the output:

```sh
python3 -m http.server 8080 --directory /tmp/resizejpeg-quality-photos
```

`djpeg` is optional. When available, the detailed report includes native
reduced-IDCT/full-IDCT controls using a frozen, non-FFI Perkeep subprocess
adapter in `internal/fastjpeg`. `CAMLI_DISABLE_DJPEG=1` disables it.
`jpegtran` is also optional. Missing tools are explicitly noted/skipped,
not treated as native/progressive coverage.

Unlike the original in-package experiment, the standalone harness cannot
inspect private worker/buffering state. Labels therefore describe the
public operation and calculated reduction factor, not observed internal
execution paths. Its candidates still call the same public decoding
operations with the same options.

The repository's BSD license covers these tools and the native adapter.
