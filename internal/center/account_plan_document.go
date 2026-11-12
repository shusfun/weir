package center

import (
	"errors"
	"strings"

	"github.com/shusfun/weir/internal/accountplan"
)

// Both direct forms and confirmed drafts validate the same complete document.
type accountPlanDocument struct {
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Grant   accountplan.Grant `json:"grant"`
}

func (d *accountPlanDocument) normalize() error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 100 {
		return errors.New("invalid_plan_name")
	}
	return d.Grant.Validate()
}
