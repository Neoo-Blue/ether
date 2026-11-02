package mcp

import (
	"fmt"
	"testing"
	"time"
)

func TestPairingAttemptBucketsAreBounded(t *testing.T) {
	p := &Pairing{attempts: map[string][]time.Time{}}
	for i := 0; i < 5000; i++ {
		p.allowAttempt(fmt.Sprintf("192.0.%d.%d:1234", i/256, i%256))
	}
	if len(p.attempts) > 4096 {
		t.Fatalf("attempt buckets unbounded: %d", len(p.attempts))
	}
}
