//go:build !linux

package layahost

import (
	"context"
	"errors"
)

func Serve(context.Context, string, string, string) error {
	return errors.New("Laya hosting requires Linux")
}
