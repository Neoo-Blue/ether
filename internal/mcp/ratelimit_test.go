package mcp

import (
	"fmt"
	"testing"
	"time"
)

func TestAttemptBucketsBounded(t *testing.T) {
	now := time.Unix(1000, 0)
	p := &Pairing{opts: PairingOptions{Now: func() time.Time { return now }}, attempts: map[string][]time.Time{}}
	for i := 0; i < 4096; i++ {
		if !p.allowAttempt(fmt.Sprintf("10.0.%d.%d:123", i/256, i%256)) {
			t.Fatalf("bucket %d rejected", i)
		}
	}
	if p.allowAttempt("10.1.1.1:123") {
		t.Fatal("accepted over cap")
	}
	for i := 1; i < 10; i++ {
		if !p.allowAttempt("10.0.0.0:456") {
			t.Fatal("existing bucket rejected early")
		}
	}
	if p.allowAttempt("10.0.0.0:789") {
		t.Fatal("per-host limit bypassed")
	}
	now = now.Add(time.Minute)
	if !p.allowAttempt("10.1.1.1:123") {
		t.Fatal("expired buckets not reclaimed")
	}
	if len(p.attempts) > 4096 {
		t.Fatal("bucket cap exceeded")
	}
}
