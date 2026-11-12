package center

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/fetch"
	"github.com/shusfun/weir/internal/netbudget"
	"github.com/shusfun/weir/packages/contracts"
)

type pacedFileResponse struct {
	http.ResponseWriter
	writer io.Writer
	bytes  int64
	err    error
}

func (w *pacedFileResponse) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.bytes += int64(n)
	if err != nil {
		w.err = err
	}
	return n, err
}

// The cluster lock bounds aggregate fallback distribution to a single stream.
// Range handling remains in ServeContent so interrupted downloads can resume.
func (s *Server) distributeFile(c *gin.Context, path string, prepare ...func(context.Context) error) {
	ctx := c.Request.Context()
	started := time.Now().UTC()
	var sent int64
	err := s.Store.Lock.Do(ctx, "center-file-distribution", func(ctx context.Context) error {
		for _, fn := range prepare {
			if err := fn(ctx); err != nil {
				return err
			}
		}
		_, budget, err := s.workerPolicy(ctx)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > relayMaximumBytes {
			return errors.New("distribution_file_invalid")
		}
		var pacer netbudget.Pacer
		pacer.SetRate(budget.BandwidthBytesPerSecond)
		writer := &pacedFileResponse{ResponseWriter: c.Writer, writer: pacer.Writer(ctx, c.Writer)}
		c.Header("Content-Type", "application/octet-stream")
		http.ServeContent(writer, c.Request, filepath.Base(path), info.ModTime(), file)
		sent = writer.bytes
		if writer.err != nil {
			return writer.err
		}
		return ctx.Err()
	})
	if err != nil && !c.Writer.Written() {
		failInternal(c, 503, "distribution_unavailable", err)
	}
	result := "ok"
	if err != nil || c.Writer.Status() >= 400 {
		result = "failed"
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, saveErr := s.Store.Ent.Audit.Create().SetAction("asset.distribute").SetTarget(itoa(gateFrom(c).ID)).SetResult(result).SetDetail(map[string]any{"trace_id": taskTrace(ctx), "origin": "center.distributeFile", "started_at": started, "finished_at": time.Now().UTC(), "body_bytes": sent, "status": c.Writer.Status()}).Save(recordCtx)
	s.tasks.note("asset-distribution-record", saveErr)
}

func (s *Server) agentKernelArchive(c *gin.Context) {
	g, ctx := gateFrom(c), c.Request.Context()
	kernel, version := c.Param("kernel"), c.Param("version")
	if !validKernel(kernel) {
		failure(c, 404, "kernel_not_found")
		return
	}
	artifact, err := s.kernelArtifact(ctx, kernel, version, g.Arch)
	if err != nil {
		failure(c, 404, "kernel_archive_not_found")
		return
	}
	destination := filepath.Join(s.dataDir(), "kernel-archives", artifact.SHA256)
	s.distributeFile(c, destination, func(ctx context.Context) error {
		if err := s.pruneKernelArchives(ctx, artifact.Size, artifact.SHA256, time.Now()); err != nil {
			return err
		}
		_, budget, err := s.workerPolicy(ctx)
		if err != nil {
			return err
		}
		var pacer netbudget.Pacer
		pacer.SetRate(budget.BandwidthBytesPerSecond)
		client := pacer.Client(githubHTTP())
		defer client.CloseIdleConnections()
		if err := fetch.Download(ctx, fetch.Request{URLs: artifact.URLs, Dest: destination, SHA256: artifact.SHA256, MaxSize: artifact.Size, HTTP: client, MinSpeed: -1}); err != nil {
			return err
		}
		info, err := os.Stat(destination)
		if err != nil {
			return err
		}
		if info.Size() != artifact.Size {
			return errors.New("kernel_archive_size_invalid")
		}
		c.Header("X-Kernel-Archive-SHA256", artifact.SHA256)
		c.Header("ETag", `"sha256:`+artifact.SHA256+`"`)
		return nil
	})
}

func (s *Server) validateKernelInstallResult(ctx context.Context, taskID string, raw []byte) error {
	var result contracts.KernelInstallResult
	if err := contracts.DecodeStrict(raw, &result, 4096); err != nil {
		return err
	}
	record, err := s.Store.Tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	body, err := s.Crypto.Decrypt(record.Ciphertext)
	if err != nil {
		return err
	}
	var req contracts.KernelInstallTask
	if err = contracts.DecodeStrict([]byte(body), &req, contracts.MaxTaskPayload); err != nil {
		return err
	}
	if result.Kernel != req.Kernel || result.Version != req.Version || result.Arch != req.Arch || result.ArchiveSHA256 != req.Artifact.SHA256 || !validTraceHex(result.BinarySHA256, 64) {
		return errors.New("kernel_install_result_invalid")
	}
	return nil
}
