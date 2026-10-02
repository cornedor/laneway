//go:build !darwin

package main

// translocatedFrom is "": only macOS runs apps from a temporary copy.
func translocatedFrom(string) string { return "" }
