package transcode

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// tsDiscontinuity restores the muxer's in-band reset on the first retained
// video and audio PES packets after a restart. Chunk preroll may have carried
// the original reset and then been discarded. Only adaptation flags change;
// elementary payload, timestamps and packet counts remain untouched.
type tsDiscontinuity struct {
	needVideo, needAudio bool
}

func (d *tsDiscontinuity) mark(path string) (result error) {
	if !d.needVideo && !d.needAudio {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	r := bufio.NewReaderSize(f, 32<<10)
	var packet [188]byte
	var offset int64
	for {
		_, err := io.ReadFull(r, packet[:])
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("incomplete TS packet: %w", err)
		}
		if packet[0] != 0x47 {
			return fmt.Errorf("invalid TS sync byte")
		}
		afc := (packet[3] >> 4) & 3
		payload := 4
		if afc&2 != 0 {
			payload = 5 + int(packet[4])
		}
		if afc&1 != 0 && packet[1]&0x40 != 0 && payload+4 <= len(packet) && packet[payload] == 0 && packet[payload+1] == 0 && packet[payload+2] == 1 {
			id := packet[payload+3]
			video := (id >= 0xe0 && id <= 0xef || id == 0xfd) && d.needVideo
			audio := (id >= 0xc0 && id <= 0xdf || id == 0xbd) && d.needAudio
			if video || audio {
				if afc&2 == 0 || packet[4] == 0 || payload > len(packet) {
					return fmt.Errorf("first retained media packet has no discontinuity flag space")
				}
				flags := packet[5] | 0x80
				if _, err := f.WriteAt([]byte{flags}, offset+5); err != nil {
					return err
				}
				if video {
					d.needVideo = false
				}
				if audio {
					d.needAudio = false
				}
				if !d.needVideo && !d.needAudio {
					return nil
				}
			}
		}
		offset += int64(len(packet))
	}
}
