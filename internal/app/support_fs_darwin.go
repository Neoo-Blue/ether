//go:build darwin

package app

import "syscall"

func supportFilesystem(path string) string {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil {
		return ""
	}
	b := make([]byte, 0, len(stat.Fstypename))
	for _, v := range stat.Fstypename {
		if v == 0 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)
}
