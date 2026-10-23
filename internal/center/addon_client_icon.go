package center

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"strings"
)

// Only bounded PNG/JPEG bytes enter the decoder. Re-encoding strips metadata,
// trailing payloads and animation; SVG and remote image URLs are not accepted.
func normalizeClientIcon(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	prefix, encoded, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") || len(encoded) > 280000 {
		return "", errors.New("invalid_catalog_icon")
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(raw) > 200*1024 {
		return "", errors.New("invalid_catalog_icon")
	}
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 512 || cfg.Height > 512 || kind != "png" && kind != "jpeg" {
		return "", errors.New("invalid_catalog_icon")
	}
	if prefix != "data:image/"+kind+";base64" {
		return "", errors.New("invalid_catalog_icon")
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return "", errors.New("invalid_catalog_icon")
	}
	// Canonical RGBA removes source-specific color and EXIF/XMP data.
	canonical := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(canonical, canonical.Bounds(), img, img.Bounds().Min, draw.Src)
	var out bytes.Buffer
	if png.Encode(&out, canonical) != nil || out.Len() > 200*1024 {
		return "", errors.New("invalid_catalog_icon")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}
