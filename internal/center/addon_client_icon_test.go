package center

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestClientIconCanonicalRaster(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	for _, kind := range []string{"png", "jpeg"} {
		var raw bytes.Buffer
		if kind == "png" {
			png.Encode(&raw, img)
		} else {
			jpeg.Encode(&raw, img, nil)
		}
		raw.WriteString("trailing-private-metadata")
		value, e := normalizeClientIcon("data:image/" + kind + ";base64," + base64.StdEncoding.EncodeToString(raw.Bytes()))
		if e != nil || !strings.HasPrefix(value, "data:image/png;base64,") {
			t.Fatal("raster rejected")
		}
		decoded, _ := base64.StdEncoding.DecodeString(strings.SplitN(value, ",", 2)[1])
		if bytes.Contains(decoded, []byte("private")) {
			t.Fatal("source metadata retained")
		}
		again, e := normalizeClientIcon(value)
		if e != nil || again != value {
			t.Fatal("canonical icon unstable")
		}
	}
	for _, value := range []string{"https://example.com/icon.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,invalid", "data:image/png;base64," + strings.Repeat("A", 280004)} {
		if _, e := normalizeClientIcon(value); e == nil {
			t.Fatal("unsafe icon accepted")
		}
	}
	var huge bytes.Buffer
	png.Encode(&huge, image.NewNRGBA(image.Rect(0, 0, 513, 1)))
	if _, e := normalizeClientIcon("data:image/png;base64," + base64.StdEncoding.EncodeToString(huge.Bytes())); e == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
