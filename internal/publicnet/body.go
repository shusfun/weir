package publicnet

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ReadBody limits both transferred content and its decoded representation.
// Clients must disable transparent decompression to enforce the wire limit.
func ReadBody(resp *http.Response, wireLimit, decodedLimit int64) ([]byte, error) {
	if wireLimit < 1 || wireLimit > 64<<20 || decodedLimit < 1 || decodedLimit > 64<<20 || resp.Uncompressed {
		return nil, errors.New("network_body_limits_invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, wireLimit+1))
	if err != nil {
		return nil, errors.New("network_body_interrupted")
	}
	if int64(len(raw)) > wireLimit {
		return nil, errors.New("network_wire_body_too_large")
	}
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
		if int64(len(raw)) > decodedLimit {
			return nil, errors.New("network_decoded_body_too_large")
		}
		return raw, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("network_gzip_invalid")
		}
		defer reader.Close()
		body, err := io.ReadAll(io.LimitReader(reader, decodedLimit+1))
		if err != nil {
			return nil, errors.New("network_gzip_invalid")
		}
		if int64(len(body)) > decodedLimit {
			return nil, errors.New("network_decoded_body_too_large")
		}
		return body, nil
	default:
		return nil, errors.New("network_encoding_unsupported")
	}
}
