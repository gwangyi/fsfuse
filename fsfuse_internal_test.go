package fsfuse

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os/user"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/gwangyi/fsx"
	"github.com/gwangyi/fsx/mockfs"
	cmockfs "github.com/gwangyi/fsx/mockfs/contextual"
	"github.com/hanwen/go-fuse/v2/fuse"
	"go.uber.org/mock/gomock"
)

func TestUtil_fillFromXFI_BadIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().AccessTime().Return(time.Unix(100, 0)).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Unix(200, 0)).AnyTimes()
	// Non-numeric IDs that will fail Atoi
	mfi.EXPECT().Owner().Return("baduid").AnyTimes()
	mfi.EXPECT().Group().Return("badgid").AnyTimes()

	var out fuse.Attr
	fillFromXFI(mfi, &out)

	// Should remain 0
	if out.Uid != 0 || out.Gid != 0 {
		t.Errorf("expected Uid/Gid 0, got %d/%d", out.Uid, out.Gid)
	}
}

func TestNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mfs := cmockfs.NewMockFileSystem(ctrl)
	inodeEmbedder := New(mfs)
	if inodeEmbedder == nil {
		t.Error("New returned nil")
	}
	n, ok := inodeEmbedder.(*node)
	if !ok {
		t.Fatal("New did not return *node")
	}
	if n.fsys != mfs {
		t.Error("New did not set fsys correctly")
	}
}

func TestNew_WithLogger(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mfs := cmockfs.NewMockFileSystem(ctrl)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	inodeEmbedder := New(mfs, Logger(logger))
	if inodeEmbedder == nil {
		t.Error("New returned nil")
	}
	n, ok := inodeEmbedder.(*node)
	if !ok {
		t.Fatal("New did not return *node")
	}
	if n.logger != logger {
		t.Error("New did not set logger correctly")
	}
}

func TestUtil_toErrno(t *testing.T) {
	tests := []struct {
		err  error
		want syscall.Errno
	}{
		{nil, 0},
		{fs.ErrNotExist, syscall.ENOENT},
		{fs.ErrPermission, syscall.EPERM},
		{fs.ErrInvalid, syscall.EINVAL},
		{fs.ErrExist, syscall.EEXIST},
		{errors.New("generic error"), syscall.EIO},
		{syscall.ENOTDIR, syscall.ENOTDIR},
		{errors.ErrUnsupported, syscall.ENOSYS},
	}

	for _, tt := range tests {
		got := toErrno(tt.err)
		if got != tt.want {
			t.Errorf("toErrno(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestUtil_fillFromStat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := &syscall.Stat_t{
		Ino:     123,
		Nlink:   2,
		Uid:     1000,
		Gid:     1000,
		Rdev:    0,
		Blksize: 4096,
		Blocks:  8,
		Atim:    syscall.Timespec{Sec: 100, Nsec: 10},
		Ctim:    syscall.Timespec{Sec: 200, Nsec: 20},
	}

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Sys().Return(st).AnyTimes()
	mfi.EXPECT().Size().Return(int64(1024)).AnyTimes()
	mfi.EXPECT().Mode().Return(fs.FileMode(0644)).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Unix(300, 30)).AnyTimes()
	mfi.EXPECT().IsDir().Return(false).AnyTimes()
	mfi.EXPECT().AccessTime().Return(time.Unix(100, 10)).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Unix(200, 20)).AnyTimes()
	mfi.EXPECT().Owner().Return("1000").AnyTimes()
	mfi.EXPECT().Group().Return("1000").AnyTimes()

	var out fuse.Attr
	statToAttr(mfi, &out)

	if out.Ino != 123 {
		t.Errorf("Ino = %d, want 123", out.Ino)
	}
	if out.Atime != 100 {
		t.Errorf("Atime = %d, want 100", out.Atime)
	}
	if out.Atimensec != 10 {
		t.Errorf("Atimensec = %d, want 10", out.Atimensec)
	}
}

func TestUtil_fillFromXFI_LookupSuccess(t *testing.T) {
	curr, err := user.Current()
	if err != nil {
		t.Skip("skipping user lookup test: ", err)
	}
	// Try to find a group too
	grp, err := user.LookupGroupId(curr.Gid)
	if err != nil {
		t.Skip("skipping group lookup test: ", err)
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().AccessTime().Return(time.Unix(100, 0)).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Unix(200, 0)).AnyTimes()
	mfi.EXPECT().Owner().Return(curr.Username).AnyTimes()
	mfi.EXPECT().Group().Return(grp.Name).AnyTimes()

	var out fuse.Attr
	fillFromXFI(mfi, &out)

	expectedUid, _ := strconv.Atoi(curr.Uid)
	expectedGid, _ := strconv.Atoi(grp.Gid)

	if out.Uid != uint32(expectedUid) {
		t.Errorf("expected Uid %d, got %d", expectedUid, out.Uid)
	}
	if out.Gid != uint32(expectedGid) {
		t.Errorf("expected Gid %d, got %d", expectedGid, out.Gid)
	}
}

func TestUtil_fillFromXFI_LookupFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().AccessTime().Return(time.Unix(100, 0)).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Unix(200, 0)).AnyTimes()
	mfi.EXPECT().Owner().Return("nonexistentuser").AnyTimes()
	mfi.EXPECT().Group().Return("nonexistentgroup").AnyTimes()

	var out fuse.Attr
	fillFromXFI(mfi, &out)
	if out.Uid != 0 || out.Gid != 0 {
		t.Errorf("expected Uid/Gid 0, got %d/%d", out.Uid, out.Gid)
	}
}

func TestUtil_statToAttr_Dir(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Size().Return(int64(4096)).AnyTimes()
	mfi.EXPECT().Mode().Return(fs.ModeDir | 0755).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().IsDir().Return(true).AnyTimes()
	mfi.EXPECT().Sys().Return(nil).AnyTimes()
	mfi.EXPECT().AccessTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().Owner().Return("1000").AnyTimes()
	mfi.EXPECT().Group().Return("1000").AnyTimes()

	var out fuse.Attr
	statToAttr(mfi, &out)

	if out.Nlink != 2 {
		t.Errorf("expected Nlink 2 for directory, got %d", out.Nlink)
	}
}

func TestUtil_statToAttr_ZeroNlink(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := &syscall.Stat_t{
		Ino:   456,
		Nlink: 0, // Zero Nlink
	}

	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Sys().Return(st).AnyTimes()
	mfi.EXPECT().Size().Return(int64(1024)).AnyTimes()
	mfi.EXPECT().Mode().Return(fs.FileMode(0644)).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Unix(300, 0)).AnyTimes()
	mfi.EXPECT().IsDir().Return(false).AnyTimes()
	mfi.EXPECT().AccessTime().Return(time.Unix(100, 0)).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Unix(200, 0)).AnyTimes()
	mfi.EXPECT().Owner().Return("1000").AnyTimes()
	mfi.EXPECT().Group().Return("1000").AnyTimes()

	var out fuse.Attr
	statToAttr(mfi, &out)

	if out.Nlink != 1 { // Default is 1
		t.Errorf("expected Nlink 1 (default), got %d", out.Nlink)
	}
}

func TestUtil_statToAttr_SimpleStat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := &syscall.Stat_t{
		Ino:   789,
		Nlink: 4,
		Atim:  syscall.Timespec{Sec: 100},
		Ctim:  syscall.Timespec{Sec: 200},
	}
	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Sys().Return(st).AnyTimes()
	mfi.EXPECT().Size().Return(int64(1024)).AnyTimes()
	mfi.EXPECT().Mode().Return(fs.FileMode(0644)).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Unix(300, 0)).AnyTimes()
	mfi.EXPECT().IsDir().Return(false).AnyTimes()

	fi := &basicFileInfo{
		FileInfo: mfi,
	}

	var out fuse.Attr
	statToAttr(fi, &out)

	if out.Ino != 789 {
		t.Errorf("expected Ino 789, got %d", out.Ino)
	}
	if out.Nlink != 4 {
		t.Errorf("expected Nlink 4, got %d", out.Nlink)
	}
}

func TestUtil_statToAttr_FallbackBasicFileInfo(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mfi := mockfs.NewMockFileInfo(ctrl)
	fi := basicFileInfo{FileInfo: mfi}

	now := time.Now()
	atime := now.Add(-time.Hour)
	mtime := now.Add(-2 * time.Hour)
	mfi.EXPECT().AccessTime().Return(atime).AnyTimes()
	mfi.EXPECT().ModTime().Return(mtime).AnyTimes()
	mfi.EXPECT().Size().Return(int64(0)).AnyTimes()
	mfi.EXPECT().Mode().Return(fs.FileMode(0644)).AnyTimes()
	mfi.EXPECT().Sys().Return(nil).AnyTimes()
	mfi.EXPECT().IsDir().Return(false).AnyTimes()

	var out fuse.Attr
	statToAttr(fi, &out)

	if out.Atime != uint64(mtime.Unix()) || out.Atimensec != uint32(mtime.Nanosecond()) {
		t.Errorf("expected Atime %d.%d (Mtime), got %d.%d", mtime.Unix(), mtime.Nanosecond(), out.Atime, out.Atimensec)
	}
}

type basicFileInfo struct {
	fsx.FileInfo
}

// Make basicFileInfo incompatible with fsx.FileInfo
func (basicFileInfo) Owner() {}

func TestUtil_basicFileInfo(t *testing.T) {
	var fi fs.FileInfo = basicFileInfo{}
	_, ok := fi.(fsx.FileInfo)
	if ok {
		t.Errorf("basicFileInfo should not be compatible with fsx.FileInfo")
	}
}

func TestUtil_toFileMode(t *testing.T) {
	tests := []struct {
		mode uint32
		want fs.FileMode
	}{
		{syscall.S_IFDIR | 0755, fs.ModeDir | 0755},
		{syscall.S_IFCHR | 0644, fs.ModeDevice | fs.ModeCharDevice | 0644},
		{syscall.S_IFBLK | 0600, fs.ModeDevice | 0600},
		{syscall.S_IFREG | 0644, 0644},
		{syscall.S_IFIFO | 0600, fs.ModeNamedPipe | 0600},
		{syscall.S_IFLNK | 0777, fs.ModeSymlink | 0777},
		{syscall.S_IFSOCK | 0600, fs.ModeSocket | 0600},
		{syscall.S_IFREG | 0644 | syscall.S_ISUID, fs.ModeSetuid | 0644},
		{syscall.S_IFREG | 0644 | syscall.S_ISGID, fs.ModeSetgid | 0644},
		{syscall.S_IFDIR | 0755 | syscall.S_ISVTX, fs.ModeDir | fs.ModeSticky | 0755},
		{syscall.S_IFDIR | 0777 | syscall.S_ISUID | syscall.S_ISGID | syscall.S_ISVTX, fs.ModeDir | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0777},
	}

	for _, tt := range tests {
		got := toFileMode(tt.mode)
		if got != tt.want {
			t.Errorf("toFileMode(%o) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestUtil_toFuseMode(t *testing.T) {
	tests := []struct {
		mode fs.FileMode
		want uint32
	}{
		{fs.ModeDir | 0755, syscall.S_IFDIR | 0755},
		{fs.ModeDevice | fs.ModeCharDevice | 0644, syscall.S_IFCHR | 0644},
		{fs.ModeDevice | 0600, syscall.S_IFBLK | 0600},
		{0644, syscall.S_IFREG | 0644},
		{fs.ModeNamedPipe | 0600, syscall.S_IFIFO | 0600},
		{fs.ModeSymlink | 0777, syscall.S_IFLNK | 0777},
		{fs.ModeSocket | 0600, syscall.S_IFSOCK | 0600},
		{fs.ModeSetuid | 0644, syscall.S_IFREG | 0644 | syscall.S_ISUID},
		{fs.ModeSetgid | 0644, syscall.S_IFREG | 0644 | syscall.S_ISGID},
		{fs.ModeDir | fs.ModeSticky | 0755, syscall.S_IFDIR | 0755 | syscall.S_ISVTX},
	}

	for _, tt := range tests {
		got := toFuseMode(tt.mode)
		if got != tt.want {
			t.Errorf("toFuseMode(%v) = %o, want %o", tt.mode, got, tt.want)
		}
	}
}
