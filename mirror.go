package fsfuse

import (
	"context"
	"strconv"

	"github.com/hanwen/go-fuse/v2/fuse"
)

// MirrorOwner returns a Mapper that returns the UID of the caller.
// It is useful for files that should appear to be owned by the user accessing them.
func MirrorOwner() func(context.Context, string) string {
	return func(ctx context.Context, s string) string {
		caller, ok := fuse.FromContext(ctx)
		if !ok {
			return ""
		}
		return strconv.Itoa(int(caller.Uid))
	}
}

// MirrorGroup returns a Mapper that returns the GID of the caller.
// It is useful for files that should appear to be owned by the group accessing them.
func MirrorGroup() func(context.Context, string) string {
	return func(ctx context.Context, s string) string {
		caller, ok := fuse.FromContext(ctx)
		if !ok {
			return ""
		}
		return strconv.Itoa(int(caller.Gid))
	}
}
