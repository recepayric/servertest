package metrics

import "sync/atomic"

var bytesOut uint64

// AddBytesOut adds n bytes to the outbound byte counter.
func AddBytesOut(n uint64) {
	atomic.AddUint64(&bytesOut, n)
}

// GetAndResetBytesOut returns bytes sent since last call and resets to 0.
func GetAndResetBytesOut() uint64 {
	return atomic.SwapUint64(&bytesOut, 0)
}
