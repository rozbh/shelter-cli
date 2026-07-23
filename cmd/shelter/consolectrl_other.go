//go:build !windows

package main

func installWindowsCtrlHandler(resetFn func()) {}
