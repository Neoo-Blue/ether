//go:build !linux

package playback

// payloadFilesystemType is only read from mountinfo on Linux.
func payloadFilesystemType(string) string { return "" }
