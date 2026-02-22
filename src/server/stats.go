package server

import (
	"sync/atomic"
)

var requestCount uint64

func incRequestCount() {
	atomic.AddUint64(&requestCount, 1)
}

func getAndResetRequestCount() uint64 {
	return atomic.SwapUint64(&requestCount, 0)
}

// GetAndResetRequestCount returns request count since last call and resets to 0.
func GetAndResetRequestCount() uint64 {
	return getAndResetRequestCount()
}
