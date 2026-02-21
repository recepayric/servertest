//go:build linux || darwin || freebsd || openbsd || netbsd

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

func run() {
	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		fmt.Printf("Getrlimit failed: %v\n", err)
		return
	}
	fmt.Printf("File descriptor limits (RLIMIT_NOFILE):\n")
	fmt.Printf("  Soft (current): %d\n", rlimit.Cur)
	fmt.Printf("  Hard (max):     %d\n", rlimit.Max)
	fmt.Printf("\n~%d concurrent connections per process (1 WS ≈ 1 FD).\n", rlimit.Cur)
	fmt.Printf("Raise: ulimit -n 65535\n")

	if runtime.GOOS == "linux" {
		f, err := os.Open("/proc/sys/fs/file-max")
		if err == nil {
			var maxOpen uint64
			fmt.Fscanf(f, "%d", &maxOpen)
			f.Close()
			fmt.Printf("\nSystem-wide max files (fs.file-max): %d\n", maxOpen)
		}
	}
}
