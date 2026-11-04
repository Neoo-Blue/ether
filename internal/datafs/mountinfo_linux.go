package datafs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// mountinfoPath is read to name the filesystem a data folder is on.
var mountinfoPath = "/proc/self/mountinfo"

// mountType is the type (fuse.shfs, nfs4, ...) of the mount
// holding dir, from the longest mount point that contains it; "" if unknown.
func mountType(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	f, err := os.Open(mountinfoPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	best, fstype := -1, ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		// 36 35 98:0 /root /mnt/point rw,noatime shared:1 - ext4 /dev/sda1 rw
		pre, post, ok := strings.Cut(sc.Text(), " - ")
		fields := strings.Fields(pre)
		after := strings.Fields(post)
		if !ok || len(fields) < 5 || len(after) < 1 {
			continue
		}
		mnt := unescapeMountinfo(fields[4])
		if !(abs == mnt || mnt == "/" || strings.HasPrefix(abs, strings.TrimSuffix(mnt, "/")+"/")) {
			continue
		}
		if len(mnt) >= best {
			best, fstype = len(mnt), after[0]
		}
	}
	return fstype
}

// unescapeMountinfo decodes the octal escapes (\040 for a space) the kernel
// writes in mountinfo paths.
func unescapeMountinfo(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			n := 0
			valid := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					valid = false
					break
				}
				n = n*8 + int(c-'0')
			}
			if valid {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
