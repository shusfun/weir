//go:build !linux

package layahost

import (
	"context"
	"errors"
)

func Prepare(context.Context, string, ...func(string)) (Deployment, error) {
	return Deployment{}, errors.New("laya_hosting_unsupported")
}
