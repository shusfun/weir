//go:build !linux

package dsh

import "context"

func lockJobProfile(context.Context, string, string) (func(), error) {
	return nil, Supported()
}
