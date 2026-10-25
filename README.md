# resizejpeg

A standalone, decoder-only fork of Go’s `image/jpeg`.

The first commit imports Go’s JPEG package unchanged. This commit removes
the encoder, replaces the inaccessible Go-internal drawing helper, and
makes the existing decoder regressions runnable as a module.

Upstream: Go commit `022c8636110ebac86a9b88724cda168ed713c21f`
(`go1.27.2`, October 8, 2026). See `LICENSE` for Go’s BSD license.
