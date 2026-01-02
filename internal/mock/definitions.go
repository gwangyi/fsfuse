package mock

import (
	"io"

	"github.com/gwangyi/fsx"
)

// FullFile is a helper interface for mock generation.
// It combines fsx.File with io.ReaderAt, io.WriterAt, and io.Seeker.
//
//go:generate mockgen -destination=mock.go -package=mock . FullFile
type FullFile interface {
	fsx.File
	io.ReaderAt
	io.WriterAt
	io.Seeker
}
