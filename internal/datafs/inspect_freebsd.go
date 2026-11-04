package datafs

import "golang.org/x/sys/unix"

func statfs(dir string) (string, uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return "", 0, err
	}
	return unix.ByteSliceToString(st.Fstypename[:]), st.Flags, nil
}

func statKind(dir string) (string, error) {
	name, flags, err := statfs(dir)
	if flags&unix.MNT_LOCAL == 0 {
		name += ",remote"
	}
	if flags&unix.MNT_UNION != 0 {
		name += ",union"
	}
	return name, err
}

func inspect(dir string) (kind, name string, class Class, err error) {
	name, flags, err := statfs(dir)
	if err != nil {
		return "", "", Unknown, err
	}
	kind, _ = statKind(dir)
	switch {
	case flags&unix.MNT_LOCAL == 0:
		class = Network
	case flags&unix.MNT_UNION != 0:
		// A union mount stacks filesystems; probe it.
	case name == "zfs" || name == "ufs" || name == "tmpfs":
		class = Local
	}
	if name == "" {
		name = "unrecognised filesystem"
	}
	return kind, name, class, nil
}
