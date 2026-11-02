package playback

import (
	"fmt"
	"strings"
)

// payloadFilesystemError is errPayloadFilesystem with, when the filesystem
// type is known, a plain explanation of what to change (#259).
func payloadFilesystemError(dir string) error {
	if hint := filesystemHint(payloadFilesystemType(dir)); hint != "" {
		return fmt.Errorf("%w: %s", errPayloadFilesystem, hint)
	}
	return errPayloadFilesystem
}

// filesystemHint names what is wrong with a filesystem type the payload
// store refuses, and how to fix it. "" when there is nothing specific to say.
func filesystemHint(fstype string) string {
	switch {
	case fstype == "fuse.shfs":
		return "Ether's data folder is on an Unraid user share (/mnt/user, fuse.shfs), which does not give the file locking Ether's database and cache need. " +
			"Map it to the pool or disk path instead, for example /mnt/cache/appdata/ether rather than /mnt/user/appdata/ether"
	case strings.HasPrefix(fstype, "nfs"), fstype == "cifs", fstype == "smb3", fstype == "smbfs":
		return "Ether's data folder is on a network share (" + fstype + "). Put it on a disk in this machine"
	case strings.HasPrefix(fstype, "fuse"):
		return "Ether's data folder is on a FUSE filesystem (" + fstype + "). Put it on a disk in this machine"
	}
	return ""
}
