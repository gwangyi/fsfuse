// Package fsfuse provides a FUSE filesystem implementation backed by a fsx.ContextualFS.
// It allows mounting any filesystem that implements the contextual.FS interface (and optional
// write interfaces) as a FUSE mount point using go-fuse.
package fsfuse

import (
	"log/slog"

	"github.com/gwangyi/fsx/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
)

type config struct {
	// logger is the sink for all internal errors and diagnostic messages.
	// It defaults to slog.Default() if not provided via options.
	logger *slog.Logger
}

// Option configures the FUSE filesystem behavior.
// Options are applied in the order they are passed to New.
type Option func(*config)

// Logger sets the structured logger to be used by the filesystem.
// This allows integrating with the application's existing logging infrastructure.
// If not specified, slog.Default() is used.
func Logger(l *slog.Logger) Option {
	return func(c *config) {
		c.logger = l
	}
}

// New creates a new FUSE root node that serves the given contextual filesystem.
// The returned InodeEmbedder can be passed to fs.Mount to mount the filesystem.
// The resulting FUSE filesystem delegates operations to the provided fsys,
// handling translation between FUSE operations and fsx interface methods.
//
// New accepts optional configuration functions (Option) to customize behavior,
// such as setting a custom logger.
func New(fsys contextual.FS, opts ...Option) fs.InodeEmbedder {
	cfg := config{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &node{
		fsys:   fsys,
		path:   ".",
		logger: cfg.logger,
	}
}
