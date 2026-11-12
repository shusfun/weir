package dsh

import "errors"

type DegradationPolicy string

const (
	DegradeReject   DegradationPolicy = "reject"
	DegradeBaseOnly DegradationPolicy = "base_only"
)

type Compatibility struct {
	InstalledVersion string            `json:"installed_version,omitempty"`
	VerifiedRange    string            `json:"verified_range"`
	OutOfRange       DegradationPolicy `json:"out_of_range"`
}

type CompatibilityDecision struct {
	Verified         bool
	EnableBase       bool
	EnableBackground bool
	Reason           string
}

func CheckCompatibility(c Compatibility) (CompatibilityDecision, error) {
	if c.OutOfRange != DegradeReject && c.OutOfRange != DegradeBaseOnly {
		return CompatibilityDecision{}, errors.New("explicit out-of-range policy is required")
	}
	if c.VerifiedRange == "" {
		return CompatibilityDecision{}, errors.New("verified range is required")
	}
	inside, err := InRange(c.InstalledVersion, c.VerifiedRange)
	if err != nil {
		return CompatibilityDecision{}, err
	}
	if inside {
		return CompatibilityDecision{Verified: true, EnableBase: true, EnableBackground: true}, nil
	}
	switch c.OutOfRange {
	case DegradeReject:
		return CompatibilityDecision{Reason: "installed version is outside the caller's verified range"}, nil
	case DegradeBaseOnly:
		return CompatibilityDecision{EnableBase: true, Reason: "installed version is outside the caller's verified range; background work is paused"}, nil
	default:
		return CompatibilityDecision{}, errors.New("explicit out-of-range policy is required")
	}
}
