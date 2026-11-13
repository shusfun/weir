//go:build !linux

package layahost

import (
	"context"
	"github.com/shusfun/weir/packages/laya"
	"os"
)

func openModelBlob(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

func inspectModelStorage(context.Context, laya.DockerExecute, string) (*ModelStorage, error) {
	return nil, failure("laya_resources_unknown", "layahost.modelStorage")
}
