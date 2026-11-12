package accountsecurity

import (
	_ "embed"
	"strings"
	"sync"
)

const DisposableSourceCommit = "ba992a0b4c867db3ff39605696bb7de9fbb2e11f"
const DisposableSourceSHA256 = "8a303e28fe2704fc20e73bd320cb9f47e8b3658adc0ebb046260d5acf59c27c2"

// https://github.com/disposable-email-domains/disposable-email-domains,
// CC0 1.0. data/LICENSE.txt accompanies the unmodified pinned source.
//
//go:embed data/disposable_email_blocklist.conf
var disposableSource string

var disposableOnce sync.Once
var disposableDomains map[string]struct{}

func bundledDisposable(domain string) bool {
	disposableOnce.Do(func() {
		disposableDomains = map[string]struct{}{}
		for _, line := range strings.Split(disposableSource, "\n") {
			d, err := NormalizeDomain(strings.TrimSpace(line))
			if err == nil {
				disposableDomains[d] = struct{}{}
			}
		}
	})
	// A listed domain also covers disposable addresses on its subdomains.
	for {
		if _, ok := disposableDomains[domain]; ok {
			return true
		}
		_, next, ok := strings.Cut(domain, ".")
		if !ok {
			return false
		}
		domain = next
	}
}
