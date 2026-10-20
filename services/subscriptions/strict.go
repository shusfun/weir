package subscriptions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Operator objects are contracts, not bags of best-effort options. Reject an
// unsupported setting rather than silently returning a different subscription.
func decodeOperator(raw json.RawMessage, dst any) error {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || value[0] != '{' {
		return errors.New("operator settings require an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("operator settings contain trailing data")
	}
	return nil
}
