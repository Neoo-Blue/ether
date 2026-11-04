package datafs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The type of the mount holding a folder comes from mountinfo: the longest
// containing mount wins and escaped paths decode (#259). Names classify
// before the magic, so a FUSE network mount is not mistaken for fuseblk.
func TestMountTypeAndClass(t *testing.T) {
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
		if got := mountType(dir); got != want {
			t.Errorf("%s: type %q, want %q", dir, got, want)
		}
	}
	const fuse = 0x65735546
	for _, tc := range []struct {
		name  string
		magic uint64
		want  Class
	}{
		{"f2fs", 0xf2f52010, Local},
		{"fuseblk", fuse, Unknown},
		{"fuse.sshfs", fuse, Network},
		{"virtiofs", fuse, Unknown},
		{"9p", 0x01021997, Unknown},
		{"nfs4", 0x6969, Network},
		{"", 0xca451a4e, Local},
		{"", 0x6969, Network},
		{"", fuse, Unknown},
	} {
		if got := classify(tc.name, tc.magic); got != tc.want {
			t.Errorf("classify(%q, %#x) = %v, want %v", tc.name, tc.magic, got, tc.want)
		}
	}
	if got := displayName("", 0x12345678); got != "unrecognised filesystem 0x12345678" {
		t.Errorf("unnamed type: %q", got)
	}
}
