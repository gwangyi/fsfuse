package fsfuse

import (
	"context"
	"log/slog"
	"path"
	"strconv"
	"syscall"
	"time"

	"github.com/gwangyi/fsx/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type node struct {
	fs.Inode
	fsys   contextual.FS
	path   string
	logger *slog.Logger
}

// Ensure node implements various FUSE node interfaces.
var _ fs.NodeGetattrer = &node{}
var _ fs.NodeLookuper = &node{}
var _ fs.NodeReaddirer = &node{}
var _ fs.NodeOpener = &node{}
var _ fs.NodeCreater = &node{}
var _ fs.NodeMkdirer = &node{}
var _ fs.NodeUnlinker = &node{}
var _ fs.NodeRmdirer = &node{}
var _ fs.NodeSymlinker = &node{}
var _ fs.NodeReadlinker = &node{}
var _ fs.NodeRenamer = &node{}
var _ fs.NodeSetattrer = &node{}

// Getattr retrieves the attributes of the node.
// It tries to use the open file handle if available to get the most up-to-date
// stats. Otherwise, it calls Lstat on the underlying filesystem.
func (n *node) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if f != nil {
		if fh, ok := f.(*fileHandle); ok {
			fi, err := fh.f.Stat()
			if err == nil {
				statToAttr(fi, &out.Attr)
				return 0
			}
		}
	}

	fi, err := contextual.Lstat(ctx, n.fsys, n.path)
	if err != nil {
		errno := toErrno(err)
		if errno != syscall.ENOENT {
			n.logger.Error("Getattr failed", "path", n.path, "error", err)
		}
		return errno
	}
	statToAttr(fi, &out.Attr)
	return 0
}

// Lookup finds a child node with the given name within the current directory.
// It returns a new node representing the child.
func (n *node) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	childPath := path.Join(n.path, name)
	fi, err := contextual.Lstat(ctx, n.fsys, childPath)
	if err != nil {
		errno := toErrno(err)
		if errno != syscall.ENOENT {
			n.logger.Error("Lookup failed", "path", childPath, "error", err)
		}
		return nil, errno
	}

	statToAttr(fi, &out.Attr)

	child := &node{
		fsys:   n.fsys,
		path:   childPath,
		logger: n.logger,
	}

	id := fs.StableAttr{
		Mode: toFuseMode(fi.Mode()),
		Ino:  out.Ino,
	}

	return n.NewInode(ctx, child, id), 0
}

// Readdir reads the contents of the directory.
// It returns a stream of directory entries.
func (n *node) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	entries, err := contextual.ReadDir(ctx, n.fsys, n.path)
	if err != nil {
		n.logger.Error("Readdir failed", "path", n.path, "error", err)
		return nil, toErrno(err)
	}

	r := make([]fuse.DirEntry, 0, len(entries))
	for _, entry := range entries {
		d := fuse.DirEntry{
			Name: entry.Name(),
			Mode: uint32(entry.Type()),
		}
		r = append(r, d)
	}
	return fs.NewListDirStream(r), 0
}

// Open opens the file associated with this node.
// It returns a FileHandle that wraps the underlying file.
func (n *node) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	f, err := contextual.OpenFile(ctx, n.fsys, n.path, int(flags), 0)
	if err != nil {
		n.logger.Error("Open failed", "path", n.path, "error", err)
		return nil, 0, toErrno(err)
	}
	return &fileHandle{f: f, logger: n.logger}, fuse.FOPEN_KEEP_CACHE, 0
}

// Create creates a new file in the directory and opens it.
// It handles mode conversion from FUSE to Go.
func (n *node) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	childPath := path.Join(n.path, name)
	f, err := contextual.OpenFile(ctx, n.fsys, childPath, int(flags)|syscall.O_CREAT, toFileMode(mode))
	if err != nil {
		n.logger.Error("Create failed", "path", childPath, "error", err)
		return nil, nil, 0, toErrno(err)
	}

	fi, err := f.Stat()
	if err != nil {
		n.logger.Error("Create: stat failed", "path", childPath, "error", err)
		_ = f.Close()
		return nil, nil, 0, toErrno(err)
	}

	statToAttr(fi, &out.Attr)

	child := &node{
		fsys:   n.fsys,
		path:   childPath,
		logger: n.logger,
	}

	id := fs.StableAttr{
		Mode: toFuseMode(fi.Mode()),
		Ino:  out.Ino,
	}

	return n.NewInode(ctx, child, id), &fileHandle{f: f, logger: n.logger}, fuse.FOPEN_KEEP_CACHE, 0
}

// Mkdir creates a new directory.
func (n *node) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	childPath := path.Join(n.path, name)
	err := contextual.Mkdir(ctx, n.fsys, childPath, toFileMode(mode))
	if err != nil {
		n.logger.Error("Mkdir failed", "path", childPath, "error", err)
		return nil, toErrno(err)
	}

	fi, err := contextual.Lstat(ctx, n.fsys, childPath)
	if err != nil {
		n.logger.Error("Mkdir: lstat failed", "path", childPath, "error", err)
		return nil, toErrno(err)
	}

	statToAttr(fi, &out.Attr)

	child := &node{
		fsys:   n.fsys,
		path:   childPath,
		logger: n.logger,
	}

	id := fs.StableAttr{
		Mode: toFuseMode(fi.Mode()),
		Ino:  out.Ino,
	}

	return n.NewInode(ctx, child, id), 0
}

// Unlink removes a file.
func (n *node) Unlink(ctx context.Context, name string) syscall.Errno {
	target := path.Join(n.path, name)
	err := contextual.Remove(ctx, n.fsys, target)
	if err != nil {
		n.logger.Error("Unlink failed", "path", target, "error", err)
	}
	return toErrno(err)
}

// Rmdir removes a directory.
func (n *node) Rmdir(ctx context.Context, name string) syscall.Errno {
	target := path.Join(n.path, name)
	err := contextual.Remove(ctx, n.fsys, target)
	if err != nil {
		n.logger.Error("Rmdir failed", "path", target, "error", err)
	}
	return toErrno(err)
}

// Symlink creates a symbolic link.
func (n *node) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	childPath := path.Join(n.path, name)
	err := contextual.Symlink(ctx, n.fsys, target, childPath)
	if err != nil {
		n.logger.Error("Symlink failed", "path", childPath, "target", target, "error", err)
		return nil, toErrno(err)
	}

	fi, err := contextual.Lstat(ctx, n.fsys, childPath)
	if err != nil {
		n.logger.Error("Symlink: lstat failed", "path", childPath, "error", err)
		return nil, toErrno(err)
	}

	statToAttr(fi, &out.Attr)

	child := &node{
		fsys:   n.fsys,
		path:   childPath,
		logger: n.logger,
	}

	id := fs.StableAttr{
		Mode: toFuseMode(fi.Mode()),
		Ino:  out.Ino,
	}

	return n.NewInode(ctx, child, id), 0
}

// Readlink reads the target of a symbolic link.
func (n *node) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	link, err := contextual.ReadLink(ctx, n.fsys, n.path)
	if err != nil {
		n.logger.Error("Readlink failed", "path", n.path, "error", err)
		return nil, toErrno(err)
	}
	return []byte(link), 0
}

// Rename renames a file or directory.
func (n *node) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	// flags are from RENAME_EXCHANGE, RENAME_NOREPLACE (Linux 3.15+)
	// fsx.Rename doesn't support flags yet.
	if flags != 0 {
		return syscall.ENOSYS
	}

	targetNode, ok := newParent.(*node)
	if !ok {
		return syscall.EXDEV
	}

	oldPath := path.Join(n.path, name)
	newPath := path.Join(targetNode.path, newName)

	err := contextual.Rename(ctx, n.fsys, oldPath, newPath)
	if err != nil {
		n.logger.Error("Rename failed", "oldPath", oldPath, "newPath", newPath, "error", err)
	}
	return toErrno(err)
}

// Setattr changes the attributes of the file (chmod, chown, utimes, truncate).
// It supports updating mode, ownership, size, and timestamps.
func (n *node) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if errno := n.chmod(ctx, in); errno != 0 {
		return errno
	}
	if errno := n.chown(ctx, in); errno != 0 {
		return errno
	}
	if errno := n.chtimes(ctx, in); errno != 0 {
		return errno
	}
	if errno := n.truncate(ctx, in); errno != 0 {
		return errno
	}
	return n.Getattr(ctx, f, out)
}

func (n *node) chmod(ctx context.Context, in *fuse.SetAttrIn) syscall.Errno {
	mode, ok := in.GetMode()
	if !ok {
		return 0
	}
	err := contextual.Chmod(ctx, n.fsys, n.path, toFileMode(mode))
	if err != nil {
		n.logger.Error("Chmod failed", "path", n.path, "error", err)
	}
	return toErrno(err)
}

func (n *node) chown(ctx context.Context, in *fuse.SetAttrIn) syscall.Errno {
	uid, uidOk := in.GetUID()
	gid, gidOk := in.GetGID()

	if !uidOk && !gidOk {
		return 0
	}

	uStr := ""
	if uidOk {
		uStr = strconv.FormatUint(uint64(uid), 10)
	}
	gStr := ""
	if gidOk {
		gStr = strconv.FormatUint(uint64(gid), 10)
	}
	err := contextual.Lchown(ctx, n.fsys, n.path, uStr, gStr)
	if err != nil {
		n.logger.Error("Chown failed", "path", n.path, "error", err)
	}
	return toErrno(err)
}

func (n *node) chtimes(ctx context.Context, in *fuse.SetAttrIn) syscall.Errno {
	mtime, mtimeOk := in.GetMTime()
	atime, atimeOk := in.GetATime()

	if !mtimeOk && !atimeOk {
		return 0
	}

	var mt, at time.Time
	if mtimeOk {
		mt = mtime
	}
	if atimeOk {
		at = atime
	}

	if !mtimeOk || !atimeOk {
		fi, err := contextual.Lstat(ctx, n.fsys, n.path)
		if err != nil {
			n.logger.Error("Chtimes: lstat failed", "path", n.path, "error", err)
			return toErrno(err)
		}
		if !mtimeOk {
			mt = fi.ModTime()
		}
		if !atimeOk {
			xfi := contextual.ExtendFileInfo(fi)
			at = xfi.AccessTime()
		}
	}

	err := contextual.Chtimes(ctx, n.fsys, n.path, at, mt)
	if err != nil {
		n.logger.Error("Chtimes failed", "path", n.path, "error", err)
	}
	return toErrno(err)
}

func (n *node) truncate(ctx context.Context, in *fuse.SetAttrIn) syscall.Errno {
	size, ok := in.GetSize()
	if !ok {
		return 0
	}
	err := contextual.Truncate(ctx, n.fsys, n.path, int64(size))
	if err != nil {
		n.logger.Error("Truncate failed", "path", n.path, "error", err)
	}
	return toErrno(err)
}
