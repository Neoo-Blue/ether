package playback

import (
	"os"

	"github.com/braedonsaunders/ether/internal/datafs"
)

func lockPayloadWitness(f *os.File) error { return datafs.TryLock(f) }
