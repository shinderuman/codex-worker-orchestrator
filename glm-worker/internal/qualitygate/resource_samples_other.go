//go:build !darwin && !linux

package qualitygate

import "runtime"

func platformHostResourceSource() hostResourceSource {
	return hostResourceSource{
		label:     "unsupported",
		supported: false,
		reason:    "host resource sampling is not implemented for " + runtime.GOOS,
		host:      ResourceSampleHost{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	}
}
