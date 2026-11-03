//go:build !linux && !darwin

package app

func supportFilesystem(string) string { return "" }
