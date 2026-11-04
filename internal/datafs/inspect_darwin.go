package datafs

import "golang.org/x/sys/unix"

func statfs(dir string) (string, bool, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return "", false, err
	}
	// Go 1.27 dropped syscall.MNT_LOCAL; x/sys still publishes the mount flag.
	return unix.ByteSliceToString(st.Fstypename[:]), st.Flags&unix.MNT_LOCAL != 0, nil
}

func statKind(dir string) (string, error) {
	name, local, err := statfs(dir)
	if !local {
		name += ",remote"
	}
	return name, err
}

func inspect(dir string) (kind, name string, class Class, err error) {
	name, local, err := statfs(dir)
	if err != nil {
		return "", "", Unknown, err
	}
	kind, _ = statKind(dir)
	switch {
	case !local:
		class = Network // smbfs, nfs, afpfs, webdav
	case name == "apfs" || name == "hfs":
		class = Local
	}
	if name == "" {
		name = "unrecognised filesystem"
	}
	return kind, name, class, nil
}
