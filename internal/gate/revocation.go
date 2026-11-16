package gate

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

var errGateRevoked = errors.New("gate identity revoked")

func (h *helperClient) Disable(ctx context.Context) error {
	var result ApplyResult
	return h.do(ctx, "POST", "/disable", nil, nil, &result)
}

func (h *Helper) disabled() bool {
	_, err := os.Stat(filepath.Join(h.P.KernelsDir, ".revoked"))
	// Fail closed when the marker cannot be read.
	return err == nil || !os.IsNotExist(err)
}

func (h *Helper) disable(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := os.MkdirAll(h.P.KernelsDir, 0755)
	if err == nil {
		var f *os.File
		f, err = os.OpenFile(filepath.Join(h.P.KernelsDir, ".revoked"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err == nil {
			_, err = f.WriteString("revoked\n")
			if err == nil {
				err = f.Sync()
			}
			err = errors.Join(err, f.Close())
		}
	}
	// Stop even when persistence fails; the caller retries the durable marker.
	h.closeBrowserTest()
	for _, name := range installableKernels {
		h.Sup.Stop(name)
	}
	if err != nil {
		writeJSON(w, 500, ApplyResult{Error: err.Error()})
		return
	}
	writeJSON(w, 200, ApplyResult{OK: true})
}
