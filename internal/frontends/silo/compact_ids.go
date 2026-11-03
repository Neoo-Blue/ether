package silo

import (
	"context"
	"errors"
	"strconv"

	"github.com/braedonsaunders/ether/internal/frontend/kit/ids"
)

// Small native IDs have their own range; larger or nonnumeric references use
// the durable compact allocator. Zero and the split point are reserved.
const compactSplit int64 = 1 << 30

func (i *instance) wireID(ctx context.Context, kind, ref string) (string, error) {
	if n, err := ids.ParseDecimal(ref); err == nil && n > 0 && n < compactSplit {
		return ref, nil
	}
	n, err := i.host.IDs.Compact(ctx, "silo", kind, ref)
	if err != nil {
		return "", err
	}
	if n <= 0 || int64(n) >= compactSplit {
		return "", errors.New("silo: compact ID capacity exhausted")
	}
	return strconv.FormatInt(compactSplit+int64(n), 10), nil
}

func (i *instance) nativeID(ctx context.Context, kind, wire string) (string, error) {
	n, err := ids.ParseDecimal(wire)
	if err != nil || n <= 0 || n > 1<<31-1 || n == compactSplit {
		return "", ids.ErrInvalid
	}
	if n < compactSplit {
		return wire, nil
	}
	return i.host.IDs.Resolve(ctx, "silo", kind, int32(n-compactSplit))
}

func (i *instance) viewerWireID(ctx context.Context, id int64) string {
	if id <= 0 {
		return ""
	}
	wire, _ := i.wireID(ctx, "viewer", strconv.FormatInt(id, 10))
	return wire
}
