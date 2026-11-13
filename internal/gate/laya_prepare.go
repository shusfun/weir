package gate

import (
	"github.com/shusfun/weir/internal/releasefiles"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

// Only the preinstalled, fixed host unit may prepare the official runtime.
// Neither a command nor a Docker argument is accepted from the agent.
func (h *Helper) prepareLayaHost(w http.ResponseWriter, r *http.Request) {
	if err := exec.CommandContext(r.Context(), "systemctl", "start", "--no-block", "weir-laya-prepare.service").Run(); err != nil {
		writeJSON(w, 503, ApplyResult{Error: "host_laya_service_unavailable"})
		return
	}
	log.Printf("laya prepare trace=%s stage=host_unit_queued", releasefiles.Trace(r.Context()))
	writeJSON(w, 202, ApplyResult{OK: true})
}
func (h *Helper) layaPreparationStatus(w http.ResponseWriter, r *http.Request) {
	raw, err := exec.CommandContext(r.Context(), "systemctl", "show", "weir-laya-prepare.service", "--property=ActiveState,Result,LoadState,InvocationID").Output()
	if err != nil {
		writeJSON(w, 503, ApplyResult{Error: "host_laya_service_unavailable"})
		return
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	state := "running"
	if values["LoadState"] != "loaded" || values["ActiveState"] == "failed" || values["Result"] != "success" {
		state = "failed"
	} else if values["ActiveState"] == "inactive" {
		state = "done"
	}
	// The fixed unit owns its own journal invocation. Correlate the polling
	// task with it without exposing arbitrary systemd environment settings.
	log.Printf("laya prepare trace=%s invocation=%s state=%s", releasefiles.Trace(r.Context()), values["InvocationID"], state)
	writeJSON(w, 200, map[string]string{"state": state, "invocation_id": values["InvocationID"]})
}
