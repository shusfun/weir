package policydata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type Values map[string]json.RawMessage

type Document struct {
	Schema   int    `json:"schema"`
	External Values `json:"external"`
	Site     Values `json:"site"`
}

type View struct {
	Revision  string            `json:"revision"`
	Document  Document          `json:"document"`
	Effective Values            `json:"effective"`
	Sources   map[string]string `json:"sources"`
}

func Empty() Document { return Document{Schema: 1, External: Values{}, Site: Values{}} }

// Resolve overlays whole registered values. Null is not a reset operation:
// removing a key from a layer restores inheritance without ambiguous merging.
func Resolve(defaults Values, doc Document, validate func(Values) error) (View, error) {
	v := View{Document: doc, Effective: Values{}, Sources: map[string]string{}}
	if doc.Schema != 1 || validate == nil {
		return v, errors.New("unsupported policy schema")
	}
	for i, layer := range []Values{defaults, doc.External, doc.Site} {
		for name, raw := range layer {
			if _, ok := defaults[name]; !ok {
				return v, fmt.Errorf("unknown policy field: %s", name)
			}
			var value any
			if json.Unmarshal(raw, &value) != nil || value == nil {
				return v, fmt.Errorf("invalid policy value: %s", name)
			}
			v.Effective[name] = append(json.RawMessage(nil), raw...)
			v.Sources[name] = []string{"default", "external", "site"}[i]
		}
		// A later override cannot hide an invalid external value.
		if err := validate(v.Effective); err != nil {
			return v, err
		}
	}
	b, err := json.Marshal(struct {
		Document  Document
		Effective Values
	}{doc, v.Effective})
	if err != nil {
		return v, err
	}
	h := sha256.Sum256(b)
	v.Revision = hex.EncodeToString(h[:])
	return v, nil
}
