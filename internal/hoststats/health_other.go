//go:build !linux

package hoststats

import "github.com/shusfun/weir/packages/contracts"

func readHealth(string) *contracts.HealthCheck {
	return &contracts.HealthCheck{}
}
