package datafs

import (
	"fmt"
	"syscall"
)

// Local filesystems by statfs magic. Each is a kernel filesystem on a disk
// (or memory) in this machine, so flock is handled by the kernel's VFS:
// exclusive between open files, released when the holder dies.
var localMagic = map[uint64]string{
	0xef53:     "ext4",
	0x58465342: "xfs",
	0x9123683e: "btrfs",
	0x01021994: "tmpfs",
	0x858458f6: "ramfs",
	0x794c7630: "overlay",
	0x2fc12fc1: "zfs",
	0xf2f52010: "f2fs",
	0xca451a4e: "bcachefs",
	0x7366746e: "ntfs3",
	0x3153464a: "jfs",
	0x52654973: "reiserfs",
	0x3434:     "nilfs2",
}

// Network filesystems by statfs magic, for when mountinfo is unreadable.
var networkMagic = map[uint64]string{
	0x6969:     "nfs",
	0x517b:     "smbfs",
	0xff534d42: "cifs",
	0xfe534d42: "smb3",
	0x00c36400: "ceph",
	0x5346414f: "afs",
	0x6b414653: "afs",
	0x73757245: "coda",
	0x01161970: "gfs2",
	0x7461636f: "ocfs2",
	0x0bd00bd0: "lustre",
	0x47504653: "gpfs",
	0x564c:     "ncpfs",
}

// Local mount type names. Names win over the magic: FUSE shares one magic
// for local (fuseblk) and remote (sshfs) filesystems.
var localName = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "tmpfs": true, "ramfs": true,
	"overlay": true, "zfs": true, "f2fs": true, "bcachefs": true, "ntfs3": true, "jfs": true,
	"reiserfs": true, "nilfs2": true,
}

func statMagic(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Type) & 0xffffffff, nil
}

func statKind(dir string) (string, error) {
	magic, err := statMagic(dir)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("0x%x", magic), nil
}

func inspect(dir string) (kind, name string, class Class, err error) {
	magic, err := statMagic(dir)
	if err != nil {
		return "", "", Unknown, err
	}
	name = mountType(dir)
	return fmt.Sprintf("0x%x", magic), displayName(name, magic), classify(name, magic), nil
}

// displayName is the mount type, else the name the magic stands for, else
// the magic in hex, so a message always says which filesystem it means.
func displayName(name string, magic uint64) string {
	if name != "" {
		return name
	}
	if n, ok := localMagic[magic]; ok {
		return n
	}
	if n, ok := networkMagic[magic]; ok {
		return n
	}
	return fmt.Sprintf("unrecognised filesystem 0x%x", magic)
}

func classify(name string, magic uint64) Class {
	switch {
	case name == "":
	case localName[name]:
		return Local
	case networkName(name):
		return Network
	default:
		// A named type we do not know (fuse.*, virtiofs, 9p, exfat...).
		// The magic must not upgrade it: fuse.sshfs is not fuseblk.
		return Unknown
	}
	if _, ok := localMagic[magic]; ok {
		return Local
	}
	if _, ok := networkMagic[magic]; ok {
		return Network
	}
	return Unknown
}
