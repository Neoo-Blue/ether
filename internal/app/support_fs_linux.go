//go:build linux

package app

import "syscall"

func supportFilesystem(path string) string {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil {
		return ""
	}
	switch uint64(stat.Type) {
	case 0xef53:
		return "ext"
	case 0x9123683e:
		return "btrfs"
	case 0x58465342:
		return "xfs"
	case 0x794c7630:
		return "overlay"
	case 0x01021994:
		return "tmpfs"
	case 0x6969:
		return "nfs"
	case 0xff534d42:
		return "cifs"
	default:
		return "unknown"
	}
}
