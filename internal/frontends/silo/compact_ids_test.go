package silo

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/braedonsaunders/ether/internal/frontend"
	"github.com/braedonsaunders/ether/internal/frontend/kit/ids"
)

type compactTestIDs struct{ refs map[string]string }

func (a compactTestIDs) Compact(_ context.Context, surface, kind, ref string) (int32, error) {
	key := surface + ":" + kind
	if old := a.refs[key]; old != "" && old != ref {
		return 0, errors.New("test allocator collision")
	}
	a.refs[key] = ref
	return 1, nil
}
func (a compactTestIDs) Resolve(_ context.Context, surface, kind string, n int32) (string, error) {
	if n != 1 || a.refs[surface+":"+kind] == "" {
		return "", errors.New("not found")
	}
	return a.refs[surface+":"+kind], nil
}

func TestSiloCompactIdentifierInvariants(t *testing.T) {
	ctx := context.Background()
	i := &instance{host: frontend.Host{IDs: ids.Codecs{Allocator: compactTestIDs{refs: map[string]string{}}}}}
	for _, tc := range []struct{ kind, ref string }{
		{"viewer", "9223372036854775807"}, {"library", strconv.FormatInt(ids.PersonalViewBit|123, 10)}, {"subtitle", "9223372036854775807"},
	} {
		wire, err := i.wireID(ctx, tc.kind, tc.ref)
		if err != nil {
			t.Fatal(err)
		}
		n, err := strconv.ParseInt(wire, 10, 32)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != wire {
			t.Fatalf("%s: %q %v", tc.kind, wire, err)
		}
		ref, err := i.nativeID(ctx, tc.kind, wire)
		if err != nil || ref != tc.ref {
			t.Fatalf("%s: %q %v", tc.kind, ref, err)
		}
	}
	guid, _ := ids.StringGUID(ids.Channel, "news")
	wire, err := i.liveChannelFileID(ctx, guid)
	if err != nil || !isLiveChannelFileID(wire) {
		t.Fatalf("live ID %q %v", wire, err)
	}
	_, resolved := i.resolveLiveChannelFileID(ctx, nil, wire)
	if resolved != guid {
		t.Fatalf("live ID resolved to %q", resolved)
	}
	for _, invalid := range []string{"0", "01", "-1", "2147483648", "1073741824"} {
		if _, err := i.nativeID(ctx, "library", invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
