// Package fsfuse provides a FUSE filesystem implementation backed by a fsx.ContextualFS.
// It allows mounting any filesystem that implements the contextual.FS interface (and optional
// write interfaces) as a FUSE mount point using go-fuse.
package fsfuse

import (
	"github.com/gwangyi/fsx/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
)

// New creates a new FUSE root node that serves the given contextual filesystem.
// The returned InodeEmbedder can be passed to fs.Mount to mount the filesystem.
// The resulting FUSE filesystem delegates operations to the provided fsys,
// handling translation between FUSE operations and fsx interface methods.
func New(fsys contextual.FS) fs.InodeEmbedder {
	return &node{
		fsys: fsys,
		path: ".",
	}
}
