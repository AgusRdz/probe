package main

import (
	"os"

	"github.com/mattn/go-isatty"
)

func isTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

const (
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorReset  = "\033[0m"
)

func bold(s string) string {
	if !isTTY() {
		return s
	}
	return colorBold + s + colorReset
}

func dim(s string) string {
	if !isTTY() {
		return s
	}
	return colorDim + s + colorReset
}

func cyan(s string) string {
	if !isTTY() {
		return s
	}
	return colorCyan + s + colorReset
}

func yellow(s string) string {
	if !isTTY() {
		return s
	}
	return colorYellow + s + colorReset
}

func red(s string) string {
	if !isTTY() {
		return s
	}
	return colorRed + s + colorReset
}

func green(s string) string {
	if !isTTY() {
		return s
	}
	return colorGreen + s + colorReset
}
