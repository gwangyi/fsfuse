package fsfuse

import (
	"errors"
	"io/fs"
	"os/user"
	"strconv"
	"syscall"

	"github.com/gwangyi/fsx"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// toErrno converts a standard Go error into a syscall.Errno.
// It handles common errors from io/fs and syscall, mapping them to appropriate
// FUSE-compatible error codes.
//
// If the error matches specific fs.Err* errors, it returns the corresponding
// syscall error (e.g., fs.ErrNotExist -> syscall.ENOENT).
// If the error can be unwrapped to a syscall.Errno, it is returned directly.
// For unknown errors, it defaults to syscall.EIO to indicate a generic I/O error.
func toErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	if errors.Is(err, fs.ErrNotExist) {
		return syscall.ENOENT
	}
	if errors.Is(err, fs.ErrPermission) {
		return syscall.EPERM
	}
	if errors.Is(err, fs.ErrInvalid) {
		return syscall.EINVAL
	}
	if errors.Is(err, fs.ErrExist) {
		return syscall.EEXIST
	}
	if errors.Is(err, errors.ErrUnsupported) {
		return syscall.ENOSYS
	}
	return syscall.EIO
}

// fillFromXFI populates the FUSE attributes from an fsx.FileInfo object.
// fsx.FileInfo provides extended attributes like AccessTime, ChangeTime, Owner, and Group.
//
// Timestamps are converted to seconds and nanoseconds.
// Owner and Group names are resolved to UIDs and GIDs.
// If the names are numeric, they are parsed directly.
// If they are usernames/groupnames, local system lookup is attempted via os/user.
// Failures in lookup leave the Uid/Gid fields as 0 (root) or their previous value.
func fillFromXFI(xfi fsx.FileInfo, out *fuse.Attr) {
	at := xfi.AccessTime()
	out.Atime = uint64(at.Unix())
	out.Atimensec = uint32(at.Nanosecond())

	ct := xfi.ChangeTime()
	out.Ctime = uint64(ct.Unix())
	out.Ctimensec = uint32(ct.Nanosecond())

	if uid, err := strconv.Atoi(xfi.Owner()); err == nil {
		out.Uid = uint32(uid)
	} else if u, err := user.Lookup(xfi.Owner()); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			out.Uid = uint32(uid)
		}
	}

	if gid, err := strconv.Atoi(xfi.Group()); err == nil {
		out.Gid = uint32(gid)
	} else if g, err := user.LookupGroup(xfi.Group()); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			out.Gid = uint32(gid)
		}
	}
}

// fillFromStat populates the FUSE attributes from a syscall.Stat_t structure.
// This is used when the underlying file info provides raw system stats.
// It copies Inode, Link count, UID, GID, Device ID, Block size, Blocks, and timestamps.
func fillFromStat(st *syscall.Stat_t, out *fuse.Attr) {
	out.Ino = st.Ino
	out.Nlink = uint32(st.Nlink)
	out.Uid = st.Uid
	out.Gid = st.Gid
	out.Rdev = uint32(st.Rdev)
	out.Blksize = uint32(st.Blksize)
	out.Blocks = uint64(st.Blocks)

	out.Atime = uint64(st.Atim.Sec)
	out.Atimensec = uint32(st.Atim.Nsec)
	out.Ctime = uint64(st.Ctim.Sec)
	out.Ctimensec = uint32(st.Ctim.Nsec)
}

// statToAttr converts a fs.FileInfo object into FUSE attributes.
// It populates the basic attributes (Size, Mode, Mtime) and attempts to retrieve
// extended attributes (Atime, Ctime, UID, GID, Inode) if available.
//
// It checks if the FileInfo implements fsx.FileInfo or provides a raw syscall.Stat_t
// via Sys(). If so, it extracts the richer metadata.
// It also ensures minimum link count for directories.
func statToAttr(fi fs.FileInfo, out *fuse.Attr) {
	// Base values from standard fs.FileInfo
	out.Size = uint64(fi.Size())
	out.Mode = toFuseMode(fi.Mode())
	out.Mtime = uint64(fi.ModTime().Unix())
	out.Mtimensec = uint32(fi.ModTime().Nanosecond())
	// Defaults
	out.Atime = out.Mtime
	out.Atimensec = out.Mtimensec
	out.Ctime = out.Mtime
	out.Ctimensec = out.Mtimensec
	out.Blksize = 4096
	out.Nlink = 1

	xfi, isXFI := fi.(fsx.FileInfo)
	st, hasStat := fi.Sys().(*syscall.Stat_t)

	// Prefer fsx.FileInfo for extended metadata (Owner/Group/Times)
	if isXFI {
		fillFromXFI(xfi, out)
	} else if hasStat {
		// Fallback to raw stat if available and fsx interface not implemented
		fillFromStat(st, out)
	} else if xfi := fsx.ExtendFileInfo(fi); xfi != nil {
		fillFromXFI(xfi, out)
	}

	// Supplement system-specific info if fi was an fsx.FileInfo
	// but also provides Sys() info (e.g. Inode number, Blocks).
	// This ensures we get both high-level metadata (like resolved usernames)
	// and low-level details (like Inode number).
	if isXFI && hasStat {
		out.Ino = st.Ino
		out.Rdev = uint32(st.Rdev)
		out.Blksize = uint32(st.Blksize)
		out.Blocks = uint64(st.Blocks)
		if st.Nlink > 0 {
			out.Nlink = uint32(st.Nlink)
		}
	}

	// Final adjustments
	// Directories must have at least 2 links (. and parent)
	if fi.IsDir() && out.Nlink < 2 {
		out.Nlink = 2
	}
}

// toFileMode converts a FUSE mode (uint32) to a Go fs.FileMode.
// FUSE passes mode flags compatible with syscall (e.g. S_IFDIR, S_IFREG),
// which need to be mapped to Go's os.Mode* constants.
// It handles standard file types and permission bits including sticky, setuid, and setgid.
func toFileMode(mode uint32) fs.FileMode {
	m := fs.FileMode(mode & 0777)
	switch mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		m |= fs.ModeDir
	case syscall.S_IFCHR:
		m |= fs.ModeDevice | fs.ModeCharDevice
	case syscall.S_IFBLK:
		m |= fs.ModeDevice
	case syscall.S_IFREG:
		// nothing to do
	case syscall.S_IFIFO:
		m |= fs.ModeNamedPipe
	case syscall.S_IFLNK:
		m |= fs.ModeSymlink
	case syscall.S_IFSOCK:
		m |= fs.ModeSocket
	}
	if mode&syscall.S_ISUID != 0 {
		m |= fs.ModeSetuid
	}
	if mode&syscall.S_ISGID != 0 {
		m |= fs.ModeSetgid
	}
	if mode&syscall.S_ISVTX != 0 {
		m |= fs.ModeSticky
	}
	return m
}

// toFuseMode converts a Go fs.FileMode to a FUSE mode (uint32).
func toFuseMode(mode fs.FileMode) uint32 {
	m := uint32(mode & 0777)
	switch {
	case mode&fs.ModeDir != 0:
		m |= syscall.S_IFDIR
	case mode&fs.ModeSymlink != 0:
		m |= syscall.S_IFLNK
	case mode&fs.ModeNamedPipe != 0:
		m |= syscall.S_IFIFO
	case mode&fs.ModeSocket != 0:
		m |= syscall.S_IFSOCK
	case mode&fs.ModeDevice != 0:
		if mode&fs.ModeCharDevice != 0 {
			m |= syscall.S_IFCHR
		} else {
			m |= syscall.S_IFBLK
		}
	default:
		m |= syscall.S_IFREG
	}
	if mode&fs.ModeSetuid != 0 {
		m |= syscall.S_ISUID
	}
	if mode&fs.ModeSetgid != 0 {
		m |= syscall.S_ISGID
	}
	if mode&fs.ModeSticky != 0 {
		m |= syscall.S_ISVTX
	}
	return m
}
