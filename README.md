# resizejpeg

Pure-Go JPEG decoding and resizing, without native libraries or subprocesses.

- `Decode(r, factor)`: reduced-IDCT decoding at factors 1, 2, 4, or 8.
- `DecodeConfig(r)`: original image dimensions and color model.

Upstream: Go commit `022c8636110ebac86a9b88724cda168ed713c21f`
(`go1.27.2`, October 8, 2026). The first commit is an unchanged upstream
import; subsequent commits contain the standalone adaptation and features.

All code uses Go’s BSD-style license in `LICENSE`. Upstream copyright
notices are retained.
