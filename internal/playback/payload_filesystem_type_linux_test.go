package playback

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An Unraid user share (/mnt/user, fuse.shfs) is refused with an
// explanation of the fix, read from the mount that holds the data folder
// (#259). The longest containing mount wins, and escaped paths decode.
func TestRefusedFilesystemNamesUnraidUserShare(t *testing.T) {
	info := filepath.Join(t.TempDir(), "mountinfo")
	lines := strings.Join([]string{
		"21 1 0:19 / / rw,relatime - overlay overlay rw",
		"300 21 0:58 /appdata/ether/config /config rw,nosuid - fuse.shfs shfs rw,user_id=0",
		"301 21 0:59 / /media\\040files rw - nfs4 nas:/media rw",
		"302 300 8:1 / /config/local rw - xfs /dev/nvme0n1p1 rw",
	}, "\n") + "\n"
	if err := os.WriteFile(info, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := mountinfoPath
	mountinfoPath = info
	t.Cleanup(func() { mountinfoPath = saved })

	for dir, want := range map[string]string{
		"/config/playback":     "fuse.shfs",
		"/config":              "fuse.shfs",
		"/config/local/x":      "xfs",
		"/media files/movies":  "nfs4",
		"/configuration/other": "overlay",
	} {
		if got := payloadFilesystemType(dir); got != want {
			t.Errorf("%s: type %q, want %q", dir, got, want)
		}
	}
	err := payloadFilesystemError("/config/playback")
	if !errors.Is(err, errPayloadFilesystem) || !strings.Contains(err.Error(), "Unraid user share") || !strings.Contains(err.Error(), "/mnt/cache/appdata/ether") {
		t.Fatalf("shfs error: %v", err)
	}
	if err := payloadFilesystemError("/media files/movies"); !strings.Contains(err.Error(), "network share (nfs4)") {
		t.Fatalf("nfs error: %v", err)
	}
	if err := payloadFilesystemError("/config/local/x"); err.Error() != errPayloadFilesystem.Error() {
		t.Fatalf("no hint expected for a local type: %v", err)
	}
}
