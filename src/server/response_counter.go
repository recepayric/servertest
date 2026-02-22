package server

import (
	"net/http"

	"servertest/metrics"
)

type responseCounter struct {
	http.ResponseWriter
}

func (w *responseCounter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	metrics.AddBytesOut(uint64(n))
	return n, err
}
