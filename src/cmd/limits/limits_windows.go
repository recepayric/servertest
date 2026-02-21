//go:build windows

package main

import "fmt"

func run() {
	fmt.Printf("Windows per-process handle limit: typically 512–2048 (varies).\n")
	fmt.Printf("Each WebSocket uses 1+ handles.\n")
	fmt.Printf("To inspect: use Process Explorer or similar.\n")
	fmt.Printf("For 50k CCU, run server on Linux (WSL or cloud) with ulimit -n 65535.\n")
}
