package main

import "fmt"

var quietOutput bool

func setQuietOutput(quiet bool) {
	quietOutput = quiet
}

func infof(format string, args ...any) {
	if quietOutput {
		return
	}
	fmt.Printf(format, args...)
}
