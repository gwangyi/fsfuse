package fsfuse_test

import (
	"errors"
	iofs "io/fs"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gwangyi/fsfuse"
	"github.com/gwangyi/fsfuse/internal/mock"
	"github.com/gwangyi/fsx/contextual"
	"github.com/gwangyi/fsx/mockfs"
	cmockfs "github.com/gwangyi/fsx/mockfs/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"go.uber.org/mock/gomock"
)

type nodeOperations interface {
	fs.InodeEmbedder
	fs.NodeGetattrer
	fs.NodeLookuper
	fs.NodeReaddirer
	fs.NodeOpener
	fs.NodeCreater
	fs.NodeMkdirer
	fs.NodeUnlinker
	fs.NodeRmdirer
	fs.NodeSymlinker
	fs.NodeReadlinker
	fs.NodeRenamer
	fs.NodeSetattrer
}

func MakeNode(t *testing.T, fsys contextual.FS, path string) nodeOperations {
	t.Helper()
	root := fsfuse.New(fsys)
	_ = fs.NewNodeFS(root, &fs.Options{})
	if path == "." || path == "" {
		return root.(nodeOperations)
	}
	node, err := root.(fs.NodeLookuper).Lookup(t.Context(), path, &fuse.EntryOut{})
	if err != syscall.Errno(0) {
		t.Fatalf("Lookup failed: %v", err)
	}
	return node.Operations().(nodeOperations)
}

func TestNode_Basic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfs := cmockfs.NewMockFileSystem(ctrl)
	ctx := t.Context()

	// Test Getattr
	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Size().Return(int64(11)).AnyTimes()
	mfi.EXPECT().Mode().Return(iofs.FileMode(0644)).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().IsDir().Return(false).AnyTimes()
	mfi.EXPECT().Name().Return("hello.txt").AnyTimes()
	mfi.EXPECT().Sys().Return(nil).AnyTimes()
	mfi.EXPECT().AccessTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().Owner().Return("1000").AnyTimes()
	mfi.EXPECT().Group().Return("1000").AnyTimes()

	mfs.EXPECT().Lstat(ctx, "hello.txt").Return(mfi, nil).Times(2)

	node := MakeNode(t, mfs, "hello.txt")

	var out fuse.AttrOut
	errno := node.Getattr(ctx, nil, &out)
	if errno != 0 {
		t.Errorf("Getattr failed: %v", errno)
	}
	if out.Size != 11 {
		t.Errorf("expected size 11, got %d", out.Size)
	}

	// Test Open and Read
	mf := mockfs.NewMockFile(ctrl)
	mfs.EXPECT().OpenFile(ctx, "hello.txt", syscall.O_RDONLY, iofs.FileMode(0)).Return(mf, nil)

	handle, _, errno := node.Open(ctx, uint32(syscall.O_RDONLY))
	if errno != 0 {
		t.Fatalf("Open failed: %v", errno)
	}

	mf.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
		return copy(b, "hello world"), nil
	})

	dest := make([]byte, 11)
	res, errno := handle.(fs.FileReader).Read(ctx, dest, 0)
	if errno != 0 {
		t.Fatalf("Read failed: %v", errno)
	}
	data, _ := res.Bytes(dest)
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(data))
	}

	mf.EXPECT().Close().Return(nil)
	if errno := handle.(fs.FileReleaser).Release(ctx); errno != 0 {
		t.Errorf("Release failed: %v", errno)
	}
}

func TestNode_Readdir(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfs := cmockfs.NewMockFileSystem(ctrl)
	ctx := t.Context()

	ent1 := mockfs.NewMockDirEntry(ctrl)
	ent1.EXPECT().Name().Return("a").AnyTimes()
	ent1.EXPECT().Type().Return(iofs.FileMode(0644)).AnyTimes()
	ent2 := mockfs.NewMockDirEntry(ctrl)
	ent2.EXPECT().Name().Return("b").AnyTimes()
	ent2.EXPECT().Type().Return(iofs.FileMode(0644)).AnyTimes()

	mfs.EXPECT().ReadDir(ctx, ".").Return([]iofs.DirEntry{ent1, ent2}, nil)

	node := MakeNode(t, mfs, ".")

	stream, errno := node.Readdir(ctx)
	if errno != 0 {
		t.Fatalf("Readdir failed: %v", errno)
	}

	var names []string
	for stream.HasNext() {
		entry, errno := stream.Next()
		if errno != 0 {
			t.Fatalf("Next failed: %v", errno)
		}
		names = append(names, entry.Name)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 entries, got %d", len(names))
	}
}

func TestNode_Operations(t *testing.T) {
	t.Run("Unlink", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().Remove(ctx, "root/file").Return(nil)
		errno := node.Unlink(ctx, "file")
		if errno != 0 {
			t.Errorf("Unlink failed: %v", errno)
		}
	})

	t.Run("Rmdir", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().Remove(ctx, "root/dir").Return(nil)
		errno := node.Rmdir(ctx, "dir")
		if errno != 0 {
			t.Errorf("Rmdir failed: %v", errno)
		}
	})

	t.Run("Readlink", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().ReadLink(ctx, "root").Return("target", nil)
		link, errno := node.Readlink(ctx)
		if errno != 0 {
			t.Errorf("Readlink failed: %v", errno)
		}
		if string(link) != "target" {
			t.Errorf("Readlink expected 'target', got '%s'", link)
		}
	})

	t.Run("Rename", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil).Times(2)
		node := MakeNode(t, mfs, "root")

		targetNode := MakeNode(t, mfs, "root")
		mfs.EXPECT().Rename(ctx, "root/old", "root/new").Return(nil)
		errno := node.Rename(ctx, "old", targetNode, "new", 0)
		if errno != 0 {
			t.Errorf("Rename failed: %v", errno)
		}
	})

	t.Run("Rename_Error_Flags", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		// Rename with flags should return ENOSYS
		errno := node.Rename(ctx, "old", nil, "new", 1)
		if errno != syscall.ENOSYS {
			t.Errorf("Rename with flags expected ENOSYS, got %v", errno)
		}
	})

	t.Run("Rename_Error_InvalidParent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		// Rename with invalid parent type should return EXDEV
		errno := node.Rename(ctx, "old", &fs.Inode{}, "new", 0)
		if errno != syscall.EXDEV {
			t.Errorf("Rename with invalid parent expected EXDEV, got %v", errno)
		}
	})

	t.Run("Setattr", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		// Test Chmod
		in := &fuse.SetAttrIn{}
		in.Valid |= fuse.FATTR_MODE
		in.Mode = 0600
		mfs.EXPECT().Chmod(ctx, "root", iofs.FileMode(0600)).Return(nil)

		// Test Chown
		in.Valid |= fuse.FATTR_UID | fuse.FATTR_GID
		in.Uid = 1001
		in.Gid = 1001
		mfs.EXPECT().Lchown(ctx, "root", "1001", "1001").Return(nil)

		// Test Truncate
		in.Valid |= fuse.FATTR_SIZE
		in.Size = 123
		mfs.EXPECT().Truncate(ctx, "root", int64(123)).Return(nil)

		// Test Chtimes (Mtime/Atime)
		in.Valid |= fuse.FATTR_MTIME | fuse.FATTR_ATIME
		in.Mtime = 1000
		in.Atime = 2000
		mfs.EXPECT().Chtimes(ctx, "root", gomock.Any(), gomock.Any()).Return(nil)

		// Expect Getattr at the end
		mfi := setupFileInfo(ctrl, "root", 123, 0600)
		mfs.EXPECT().Lstat(ctx, "root").Return(mfi, nil)

		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != 0 {
			t.Errorf("Setattr failed: %v", errno)
		}
	})

	t.Run("Setattr_Partial_Times", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_MTIME
		in.Mtime = 1234

		mfi := setupFileInfo(ctrl, "root", 0, 0644)
		mfs.EXPECT().Lstat(ctx, "root").Return(mfi, nil).Times(2) // One for current times, one for Getattr result
		mfs.EXPECT().Chtimes(ctx, "root", gomock.Any(), gomock.Any()).Return(nil)

		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != 0 {
			t.Errorf("Setattr failed: %v", errno)
		}
	})

	t.Run("Setattr_Uid_Only", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_UID
		in.Uid = 1001
		mfs.EXPECT().Lchown(ctx, "root", "1001", "").Return(nil)
		mfi := setupFileInfo(ctrl, "root", 0, 0644)
		mfs.EXPECT().Lstat(ctx, "root").Return(mfi, nil)
		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != 0 {
			t.Errorf("Setattr failed: %v", errno)
		}
	})

	t.Run("Setattr_Errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		// Chmod error
		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_MODE
		in.Mode = 0644
		mfs.EXPECT().Chmod(ctx, "root", iofs.FileMode(0644)).Return(errors.New("fail"))

		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != syscall.EIO {
			t.Errorf("expected EIO for Chmod, got %v", errno)
		}

		// Chown error
		in = &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_UID
		in.Uid = 1000
		mfs.EXPECT().Lchown(ctx, "root", "1000", "").Return(errors.New("fail"))
		if errno := node.Setattr(ctx, nil, in, &out); errno != syscall.EIO {
			t.Errorf("expected EIO for Chown, got %v", errno)
		}

		// Truncate error
		in = &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_SIZE
		in.Size = 123
		mfs.EXPECT().Truncate(ctx, "root", int64(123)).Return(errors.New("fail"))
		if errno := node.Setattr(ctx, nil, in, &out); errno != syscall.EIO {
			t.Errorf("expected EIO for Truncate, got %v", errno)
		}

		// Chtimes error
		in = &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_MTIME | fuse.FATTR_ATIME
		in.Mtime = 100
		in.Atime = 200
		mfs.EXPECT().Chtimes(ctx, "root", gomock.Any(), gomock.Any()).Return(errors.New("fail"))
		if errno := node.Setattr(ctx, nil, in, &out); errno != syscall.EIO {
			t.Errorf("expected EIO for Chtimes, got %v", errno)
		}
	})

	t.Run("Getattr_With_FileHandle", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		mfs.EXPECT().OpenFile(gomock.Any(), "root", gomock.Any(), gomock.Any()).Return(m, nil)
		node := MakeNode(t, mfs, "root")
		fh, _, err := node.Open(t.Context(), uint32(os.O_RDWR))
		if err != syscall.Errno(0) {
			t.Fatalf("Open failed: %v", err)
		}

		mfi := setupFileInfo(ctrl, "file", 123, 0644)
		m.EXPECT().Stat().Return(mfi, nil)

		var out fuse.AttrOut
		errno := node.Getattr(ctx, fh, &out)
		if errno != 0 {
			t.Errorf("Getattr with FH failed: %v", errno)
		}
		if out.Size != 123 {
			t.Errorf("Expected size 123, got %d", out.Size)
		}
	})

	t.Run("Getattr_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().Lstat(ctx, "root").Return(nil, iofs.ErrNotExist)
		var out fuse.AttrOut
		if errno := node.Getattr(ctx, nil, &out); errno != syscall.ENOENT {
			t.Errorf("expected ENOENT, got %v", errno)
		}
	})

	t.Run("Open_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().OpenFile(ctx, "root", gomock.Any(), gomock.Any()).Return(nil, errors.New("open error"))
		_, _, errno := node.Open(ctx, 0)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Readdir_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().ReadDir(ctx, "root").Return(nil, iofs.ErrPermission)
		_, errno := node.Readdir(ctx)
		if errno != syscall.EPERM {
			t.Errorf("expected EPERM, got %v", errno)
		}
	})

	t.Run("Readlink_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		mfs.EXPECT().ReadLink(ctx, "root").Return("", iofs.ErrInvalid)
		_, errno := node.Readlink(ctx)
		if errno != syscall.EINVAL {
			t.Errorf("expected EINVAL, got %v", errno)
		}
	})

	t.Run("Setattr_Gid_Only", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_GID
		in.Gid = 1001
		mfs.EXPECT().Lchown(ctx, "root", "", "1001").Return(nil)
		mfi := setupFileInfo(ctrl, "root", 0, 0644)
		mfs.EXPECT().Lstat(ctx, "root").Return(mfi, nil)
		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != 0 {
			t.Errorf("Setattr failed: %v", errno)
		}
	})

	t.Run("Setattr_Atime_Only", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_ATIME
		in.Atime = 1234

		mfi := setupFileInfo(ctrl, "root", 0, 0644)
		mfs.EXPECT().Lstat(ctx, "root").Return(mfi, nil).Times(2)
		mfs.EXPECT().Chtimes(ctx, "root", gomock.Any(), gomock.Any()).Return(nil)

		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != 0 {
			t.Errorf("Setattr failed: %v", errno)
		}
	})

	t.Run("Setattr_Error_Lstat", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfiRoot := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfiRoot, nil)
		node := MakeNode(t, mfs, "root")

		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_MTIME
		in.Mtime = 1234

		mfs.EXPECT().Lstat(ctx, "root").Return(nil, errors.New("lstat fail"))

		var out fuse.AttrOut
		if errno := node.Setattr(ctx, nil, in, &out); errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})
}

func TestNode_WithBridge(t *testing.T) {
	// Tests needing NewNodeFS
	t.Run("Lookup", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		childMfi := setupFileInfo(ctrl, "child", 100, 0644)
		mfs.EXPECT().Lstat(gomock.Any(), "root/child").Return(childMfi, nil)

		var out fuse.EntryOut
		childInode, errno := rootNode.Lookup(ctx, "child", &out)
		if errno != 0 {
			t.Errorf("Lookup failed: %v", errno)
		}
		if childInode == nil {
			t.Fatal("Lookup returned nil inode")
		}
	})

	t.Run("Mkdir", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Mkdir(gomock.Any(), "root/newdir", iofs.FileMode(0755)).Return(nil)
		childMfi := setupFileInfo(ctrl, "newdir", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root/newdir").Return(childMfi, nil)

		var out fuse.EntryOut
		childInode, errno := rootNode.Mkdir(ctx, "newdir", 0755, &out)
		if errno != 0 {
			t.Errorf("Mkdir failed: %v", errno)
		}
		if childInode == nil {
			t.Fatal("Mkdir returned nil inode")
		}
	})

	t.Run("Create", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mf := mockfs.NewMockFile(ctrl)
		childMfi := setupFileInfo(ctrl, "newfile", 0, 0644)

		mf.EXPECT().Stat().Return(childMfi, nil)
		mfs.EXPECT().OpenFile(gomock.Any(), "root/newfile", gomock.Any(), iofs.FileMode(0644)).Return(mf, nil)

		var out fuse.EntryOut
		childInode, handle, _, errno := rootNode.Create(ctx, "newfile", 0, 0644, &out)
		if errno != 0 {
			t.Errorf("Create failed: %v", errno)
		}
		if childInode == nil {
			t.Fatal("Create returned nil inode")
		}
		if handle == nil {
			t.Error("Create returned nil handle")
		}
	})

	t.Run("Symlink", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Symlink(gomock.Any(), "target", "root/sym").Return(nil)
		childMfi := setupFileInfo(ctrl, "sym", 0, iofs.ModeSymlink|0777)
		mfs.EXPECT().Lstat(gomock.Any(), "root/sym").Return(childMfi, nil)

		var out fuse.EntryOut
		childInode, errno := rootNode.Symlink(ctx, "target", "sym", &out)
		if errno != 0 {
			t.Errorf("Symlink failed: %v", errno)
		}
		if childInode == nil {
			t.Fatal("Symlink returned nil inode")
		}
	})

	t.Run("Lookup_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Lstat(gomock.Any(), "root/missing").Return(nil, iofs.ErrNotExist)
		var out fuse.EntryOut
		_, errno := rootNode.Lookup(ctx, "missing", &out)
		if errno != syscall.ENOENT {
			t.Errorf("expected ENOENT, got %v", errno)
		}
	})

	t.Run("Mkdir_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Mkdir(gomock.Any(), "root/fail", gomock.Any()).Return(iofs.ErrPermission)
		var out fuse.EntryOut
		_, errno := rootNode.Mkdir(ctx, "fail", 0755, &out)
		if errno != syscall.EPERM {
			t.Errorf("expected EPERM, got %v", errno)
		}
	})

	t.Run("Mkdir_LstatError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Mkdir(gomock.Any(), "root/dir", iofs.FileMode(0755)).Return(nil)
		// Mkdir succeeds, but subsequent Lstat fails
		mfs.EXPECT().Lstat(gomock.Any(), "root/dir").Return(nil, errors.New("lstat fail"))

		var out fuse.EntryOut
		_, errno := rootNode.Mkdir(ctx, "dir", 0755, &out)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Symlink_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Symlink(gomock.Any(), "target", "root/fail").Return(errors.New("fail"))
		var out fuse.EntryOut
		_, errno := rootNode.Symlink(ctx, "target", "fail", &out)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Symlink_LstatError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().Symlink(gomock.Any(), "target", "root/sym").Return(nil)
		// Symlink succeeds, but subsequent Lstat fails
		mfs.EXPECT().Lstat(gomock.Any(), "root/sym").Return(nil, errors.New("lstat fail"))

		var out fuse.EntryOut
		_, errno := rootNode.Symlink(ctx, "target", "sym", &out)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Create_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mfs.EXPECT().OpenFile(gomock.Any(), "root/fail", gomock.Any(), gomock.Any()).Return(nil, errors.New("fail"))
		var out fuse.EntryOut
		_, _, _, errno := rootNode.Create(ctx, "fail", 0, 0644, &out)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Create_StatError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()

		mfs := cmockfs.NewMockFileSystem(ctrl)
		mfi := setupFileInfo(ctrl, "root", 0, iofs.ModeDir|0755)
		mfs.EXPECT().Lstat(gomock.Any(), "root").Return(mfi, nil).AnyTimes()
		rootNode := MakeNode(t, mfs, "root")

		mf := mockfs.NewMockFile(ctrl)
		mfs.EXPECT().OpenFile(ctx, "root/fail", gomock.Any(), gomock.Any()).Return(mf, nil)
		mf.EXPECT().Stat().Return(nil, errors.New("stat fail"))
		mf.EXPECT().Close().Return(nil)

		var out fuse.EntryOut
		_, _, _, errno := rootNode.Create(ctx, "fail", 0, 0644, &out)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})
}
