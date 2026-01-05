package fsfuse

import (
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestMirrorOwner(t *testing.T) {
	// Case 1: Context with caller
	caller := &fuse.Caller{Owner: fuse.Owner{Uid: 1234, Gid: 5678}}
	ctx := fuse.NewContext(t.Context(), caller)

	mapper := MirrorOwner()
	got := mapper(ctx, "any")
	if got != "1234" {
		t.Errorf("MirrorOwner() = %q, want %q", got, "1234")
	}

	// Case 2: Context without caller
	ctxWithoutCaller := t.Context()
	got = mapper(ctxWithoutCaller, "any")
	if got != "" {
		t.Errorf("MirrorOwner() = %q, want %q", got, "")
	}
}

func TestMirrorGroup(t *testing.T) {
	// Case 1: Context with caller
	caller := &fuse.Caller{Owner: fuse.Owner{Uid: 1234, Gid: 5678}}
	ctx := fuse.NewContext(t.Context(), caller)

	mapper := MirrorGroup()
	got := mapper(ctx, "any")
	if got != "5678" {
		t.Errorf("MirrorGroup() = %q, want %q", got, "5678")
	}

	// Case 2: Context without caller
	ctxWithoutCaller := t.Context()
	got = mapper(ctxWithoutCaller, "any")
	if got != "" {
		t.Errorf("MirrorGroup() = %q, want %q", got, "")
	}
}
