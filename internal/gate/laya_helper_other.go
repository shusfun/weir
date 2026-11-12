//go:build !linux

package gate

import (
	"context"
	"github.com/shusfun/weir/internal/layahost"
)

func readLayaDeployment(dir string) (layaDeployment, error) { return layahost.ReadDeployment(dir) }
func executeLayaDocker(ctx context.Context, args []string) ([]byte, error) {
	return layahost.ExecuteDocker(ctx, args)
}
