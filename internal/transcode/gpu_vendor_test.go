package transcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGPUVendorFromSysfs(t *testing.T) {
	root := t.TempDir()
	for node, id := range map[string]string{"renderD128": "0x8086\n", "renderD129": "0x1002\n", "renderD130": "0x1af4\n"} {
		dir := filepath.Join(root, node, "device")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte(id), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved := sysfsDRM
	sysfsDRM = root
	t.Cleanup(func() { sysfsDRM = saved })
	for device, want := range map[string]string{
		"/dev/dri/renderD128": "intel",
		"/dev/dri/renderD129": "amd",
		"/dev/dri/renderD130": "",
		"/dev/dri/renderD131": "",
		"":                    "",
	} {
		if got := GPUVendor(device); got != want {
			t.Errorf("%q: vendor %q, want %q", device, got, want)
		}
	}
}
