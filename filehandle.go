package fsfuse

import (
	"context"
	"errors"
	"io"
	"sync"
	"syscall"

	"github.com/gwangyi/fsx/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// fileHandle wraps a contextual.File to serve FUSE read/write requests.
// It maintains an internal offset for files that do not support Seeking (e.g. streams),
// allowing sequential read/write operations to work via fallback logic.
type fileHandle struct {
	f      contextual.File
	offset int64
	mu     sync.Mutex
}

var _ fs.FileReader = &fileHandle{}
var _ fs.FileWriter = &fileHandle{}
var _ fs.FileReleaser = &fileHandle{}
var _ fs.FileFlusher = &fileHandle{}

// Read reads data from the file at the given offset.
//
// It attempts to use io.ReaderAt first.
// If not supported, it tries io.Seeker to seek to the offset.
// If neither are supported (e.g. pipe), it simulates seeking by reading and discarding data
// until the desired offset is reached (if moving forward).
// Backward seeks on non-seekable files return ENOSYS.
func (fh *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	if ra, ok := fh.f.(io.ReaderAt); ok {
		n, err := ra.ReadAt(dest, off)
		if !errors.Is(err, errors.ErrUnsupported) {
			if err != nil && err != io.EOF {
				return nil, toErrno(err)
			}
			return fuse.ReadResultData(dest[:n]), 0
		}
	}

	if s, ok := fh.f.(io.Seeker); ok {
		if _, err := s.Seek(off, io.SeekStart); err != nil {
			return nil, toErrno(err)
		}
		n, err := fh.f.Read(dest)
		if err != nil && err != io.EOF {
			return nil, toErrno(err)
		}
		return fuse.ReadResultData(dest[:n]), 0
	}

	if off < fh.offset {
		return nil, syscall.ENOSYS
	}
	if off > fh.offset {
		n, err := io.CopyN(io.Discard, fh.f, off-fh.offset)
		fh.offset += n
		if err != nil {
			if err == io.EOF {
				return fuse.ReadResultData(nil), 0
			}
			return nil, toErrno(err)
		}
	}

	n, err := fh.f.Read(dest)
	if n > 0 {
		fh.offset += int64(n)
	}
	if err != nil && err != io.EOF {
		return nil, toErrno(err)
	}
	return fuse.ReadResultData(dest[:n]), 0
}

// Write writes data to the file at the given offset.
//
// It attempts to use io.WriterAt first.
// If not supported, it tries io.Seeker.
// If neither are supported, it simulates seeking forward by writing zeros (padding)
// to fill the gap between the current offset and the requested offset.
// Backward seeks on non-seekable files return ENOSYS.
func (fh *fileHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	fh.mu.Lock()
	defer fh.mu.Unlock()

	if wa, ok := fh.f.(io.WriterAt); ok {
		n, err := wa.WriteAt(data, off)
		if !errors.Is(err, errors.ErrUnsupported) {
			return uint32(n), toErrno(err)
		}
	}

	if s, ok := fh.f.(io.Seeker); ok {
		if _, err := s.Seek(off, io.SeekStart); err != nil {
			return 0, toErrno(err)
		}
		n, err := fh.f.(io.Writer).Write(data)
		return uint32(n), toErrno(err)
	}

	if off < fh.offset {
		return 0, syscall.ENOSYS
	}
	if off > fh.offset {
		zeros := make([]byte, 4096)
		remaining := off - fh.offset
		for remaining > 0 {
			toWrite := min(remaining, int64(len(zeros)))
			n, err := fh.f.(io.Writer).Write(zeros[:toWrite])
			if n > 0 {
				fh.offset += int64(n)
				remaining -= int64(n)
			}
			if err != nil {
				return 0, toErrno(err)
			}
		}
	}

	n, err := fh.f.(io.Writer).Write(data)
	if n > 0 {
		fh.offset += int64(n)
	}
	return uint32(n), toErrno(err)
}

// Flush is called when the file is closed or flushed.
// It returns 0 as fsx does not currently expose explicit Flush.
func (fh *fileHandle) Flush(ctx context.Context) syscall.Errno {
	return 0
}

// Release closes the file handle.
func (fh *fileHandle) Release(ctx context.Context) syscall.Errno {
	return toErrno(fh.f.Close())
}
