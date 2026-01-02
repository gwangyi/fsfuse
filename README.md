# fsfuse

[![Go Reference](https://pkg.go.dev/badge/github.com/gwangyi/fsfuse.svg)](https://pkg.go.dev/github.com/gwangyi/fsfuse)
[![Go Report Card](https://goreportcard.com/badge/github.com/gwangyi/fsfuse)](https://goreportcard.com/report/github.com/gwangyi/fsfuse)
[![Coverage Status](https://coveralls.io/repos/github/gwangyi/fsfuse/badge.svg?branch=main)](https://coveralls.io/github/gwangyi/fsfuse?branch=main)

`fsfuse` is a Go library that bridges the [fsx](https://github.com/gwangyi/fsx) extended filesystem interfaces to [FUSE](https://libfuse.github.io/doxygen/index.html) (Filesystem in Userspace). It allows you to mount any filesystem implementation that follows the `contextual.FS` interface as a native filesystem on your operating system using [go-fuse](https://github.com/hanwen/go-fuse).

## Features

- **Context-Aware**: Full support for `context.Context` throughout the filesystem operations, allowing for cancellation and timeout propagation to the underlying storage.
- **Read/Write Support**: Implements FUSE operations for reading, writing, creating, and deleting files and directories.
- **Rich Metadata**: Maps extended file information (`fsx.FileInfo`) including UID, GID, Access Time, and Change Time to FUSE attributes.
- **Stream Support**: Built-in fallback logic for non-seekable files (e.g., pipes, sockets, or sequential streams). `Read` can simulate seeking forward by discarding data, and `Write` can pad with zeros.
- **High Reliability**: Maintained with 100% statement coverage and rigorous unit/E2E testing.

## Installation

```bash
go get github.com/gwangyi/fsfuse
```

## Quick Start

The following example demonstrates how to mount a `contextual.FS` (wrapped from an `osfs`) to a local directory.

```go
package main

import (
	"log"
	"os"

	"github.com/gwangyi/fsfuse"
	"github.com/gwangyi/fsx/contextual"
	"github.com/gwangyi/fsx/osfs"
	"github.com/hanwen/go-fuse/v2/fs"
)

func main() {
	// 1. Prepare your backing filesystem (e.g., osfs)
	backing, err := osfs.New("/path/to/source")
	if err != nil {
		log.Fatal(err)
	}

	// 2. Wrap it with contextual support
	fsys := contextual.ToContextual(backing)

	// 3. Create the FUSE root node
	root := fsfuse.New(fsys)

	// 4. Mount the filesystem
	opts := &fs.Options{}
	server, err := fs.Mount("/path/to/mountpoint", root, opts)
	if err != nil {
		log.Fatalf("Mount failed: %v", err)
	}

	// 5. Wait for the server to be unmounted
	log.Println("Filesystem mounted. Press Ctrl+C to unmount.")
	server.Wait()
}
```

## Advanced Logic: Non-Seekable Files

`fsfuse` includes sophisticated handling for underlying files that do not implement `io.Seeker` or `io.ReaderAt`/`io.WriterAt`. 

- **Read**: If a read is requested at an offset greater than the current position, `fsfuse` will read and discard the intermediate data to reach the target offset. If the offset is behind the current position, it returns `ENOSYS`.
- **Write**: If a write is requested at a forward offset, `fsfuse` will pad the gap with zero bytes before performing the write.

## Testing

To run the tests, ensure you have FUSE installed on your system (e.g., `libfuse3-dev` on Ubuntu).

```bash
# Run all tests
go test -v ./...

# Run tests with coverage report
go test -v -coverprofile=coverage.out .
go tool cover -html=coverage.out
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
