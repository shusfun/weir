//go:build !linux

package layahost

import (
	"context"
	"errors"
)

func ReadDeployment(string) (Deployment, error) {
	return Deployment{}, errors.New("Laya hosting requires Linux")
}
func ExecuteDocker(context.Context, []string) ([]byte, error) {
	return nil, errors.New("Laya hosting requires Linux")
}

func TrustedDirectory(string) error { return errors.New("requires Linux") }
func TrustedFile(string) error      { return errors.New("requires Linux") }
