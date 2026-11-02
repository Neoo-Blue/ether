package transcode

import (
	"os"
	"path/filepath"
	"strings"
)

// sysfsDRM is where Linux lists DRM devices; tests point it elsewhere.
var sysfsDRM = "/sys/class/drm"

// GPUVendor names the vendor of a DRM render node ("/dev/dri/renderD128")
// from its PCI vendor id in sysfs: "intel", "amd", "nvidia", or "" when it
// is unknown or not on Linux. VA-API runs on Intel and AMD alike, so the
// backend name alone does not say whose hardware it is (#261).
func GPUVendor(device string) string {
	if device == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(sysfsDRM, filepath.Base(device), "device", "vendor"))
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(string(b))) {
	case "0x8086":
		return "intel"
	case "0x1002", "0x1022":
		return "amd"
	case "0x10de":
		return "nvidia"
	}
	return ""
}
