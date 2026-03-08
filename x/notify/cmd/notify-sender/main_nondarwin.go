//go:build !darwin
// +build !darwin

package main

import (
	"flag"
	"fmt"
)

func main() {
	flag.Parse()
	fmt.Println("notify-sender helper is only supported on macOS")
}
