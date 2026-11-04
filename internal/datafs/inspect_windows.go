package datafs

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func statKind(dir string) (string, error) {
	kind, _, _, err := inspect(dir)
	return kind, err
}

func inspect(dir string) (kind, name string, class Class, err error) {
	f, err := os.Open(dir)
	if err != nil {
		return "", "", Unknown, err
	}
	defer f.Close()
	var final [4096]uint16
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &final[0], uint32(len(final)), 0)
	if err != nil {
		return "", "", Unknown, err
	}
	if n >= uint32(len(final)) {
		return "long-path", "unrecognised filesystem", Unknown, nil
	}
	resolved := windows.UTF16ToString(final[:])
	if strings.HasPrefix(strings.ToUpper(resolved), `\\?\UNC\`) {
		return "unc", "SMB share", Network, nil
	}
	resolved = strings.TrimPrefix(resolved, `\\?\`)
	root := filepath.VolumeName(resolved) + `\`
	if strings.HasPrefix(root, `\\`) {
		return "unc", "SMB share", Network, nil
	}
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", "", Unknown, err
	}
	drive := windows.GetDriveType(ptr)
	if drive == windows.DRIVE_REMOTE {
		return "remote", "network drive", Network, nil
	}
	var fsName [64]uint16
	if err := windows.GetVolumeInformationByHandle(windows.Handle(f.Fd()), nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))); err != nil {
		return "", "", Unknown, err
	}
	name = windows.UTF16ToString(fsName[:])
	if name == "" {
		name = "unrecognised filesystem"
	}
	local := drive == windows.DRIVE_FIXED || drive == windows.DRIVE_REMOVABLE
	if local && (name == "NTFS" || name == "ReFS") {
		class = Local
	}
	return name + "," + root, name, class, nil
}
