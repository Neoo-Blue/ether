package transcode

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTSRetainedBoundaryMarksMediaWithoutChangingPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "segment.ts")
	before := testTSChunkAdapted(4*time.Second, "video")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	d := tsDiscontinuity{needVideo: true}
	if err := d.mark(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.needVideo || after[5]&0x80 == 0 {
		t.Fatal("retained PES lacks the discontinuity marker")
	}
	after[5] &^= 0x80
	if !bytes.Equal(before, after) {
		t.Fatal("marking changed media payload or timestamps")
	}
	if err := os.WriteFile(path, testTSChunk(4*time.Second, "no-adaptation"), 0600); err != nil {
		t.Fatal(err)
	}
	d.needVideo = true
	if err := d.mark(path); err == nil {
		t.Fatal("published a PES without space for a safe discontinuity marker")
	}
}
