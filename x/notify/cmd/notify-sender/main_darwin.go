//go:build darwin
// +build darwin

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	title := flag.String("title", "", "notification title")
	subtitle := flag.String("subtitle", "", "notification subtitle")
	body := flag.String("body", "", "notification body")
	icon := flag.String("icon", "", "path to icon file to attach")
	delay := flag.Int("delay", 1, "delay seconds before delivery")
	flag.Parse()

	if err := SendNativeNotification(*title, *subtitle, *body, *icon, *delay); err != nil {
		fmt.Fprintln(os.Stderr, "failed to send notification:", err)
		os.Exit(1)
	}
}
