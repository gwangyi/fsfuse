package fsfuse_test

import (
	"errors"
	"io"
	"os"
	"syscall"
	"testing"

	"github.com/gwangyi/fsfuse/internal/mock"
	"github.com/gwangyi/fsx"
	"github.com/gwangyi/fsx/mockfs"
	cmockfs "github.com/gwangyi/fsx/mockfs/contextual"
	"github.com/hanwen/go-fuse/v2/fs"
	"go.uber.org/mock/gomock"
)

type filehandle interface {
	fs.FileReader
	fs.FileWriter
	fs.FileReleaser
	fs.FileFlusher
}

func MakeFileHandle(t *testing.T, ctrl *gomock.Controller, file fsx.File) filehandle {
	t.Helper()
	mfs := cmockfs.NewMockFileSystem(ctrl)
	mfi := setupFileInfo(ctrl, "file", 0, 0644)
	mfs.EXPECT().Lstat(gomock.Any(), "file").Return(mfi, nil).AnyTimes()
	mfs.EXPECT().OpenFile(gomock.Any(), "file", gomock.Any(), gomock.Any()).Return(file, nil)
	node := MakeNode(t, mfs, "file")
	fh, _, err := node.Open(t.Context(), uint32(os.O_RDWR))
	if err != syscall.Errno(0) {
		t.Fatalf("Open failed: %v", err)
	}
	return fh.(filehandle)
}

func TestFileHandle_Read(t *testing.T) {
	t.Run("ReaderAt_Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().ReadAt(gomock.Any(), int64(0)).DoAndReturn(func(p []byte, off int64) (int, error) {
			return copy(p, "data"), nil
		})

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 0)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if string(d) != "data" {
			t.Errorf("Expected 'data', got %s", d)
		}
	})

	t.Run("ReaderAt_EOF", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().ReadAt(gomock.Any(), int64(0)).DoAndReturn(func(p []byte, off int64) (int, error) {
			copy(p, "short")
			return 5, io.EOF
		})

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 0)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if string(d[:5]) != "short" {
			t.Errorf("Expected 'short', got %s", d[:5])
		}
	})

	t.Run("ReaderAt_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)
		m.EXPECT().ReadAt(gomock.Any(), int64(0)).Return(0, errors.New("fail"))

		_, errno := fh.Read(ctx, make([]byte, 10), 0)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Seeker_Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		// Force fallthrough to Seeker
		m.EXPECT().ReadAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(5), nil)
		m.EXPECT().Read(gomock.Any()).Return(3, nil)

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 5)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if len(d) != 3 {
			t.Errorf("Expected 3 bytes, got %d", len(d))
		}
	})

	t.Run("Seeker_Seek_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().ReadAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(0), errors.New("seek fail"))

		_, errno := fh.Read(ctx, make([]byte, 10), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Seeker_Read_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().ReadAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(5), nil)
		m.EXPECT().Read(gomock.Any()).Return(0, errors.New("read fail"))

		_, errno := fh.Read(ctx, make([]byte, 10), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Success_Sequential", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		// mockfs.MockFile does NOT implement Seeker or ReaderAt
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			return copy(b, "data"), nil
		})

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 0)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if string(d) != "data" {
			t.Errorf("Expected 'data', got %s", d)
		}
		// skip offset since it's not exported
	})

	t.Run("Fallback_Success_Skip", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		// Skip 5 bytes
		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			if len(b) != 5 {
				t.Errorf("Skip read length mismatch: got %d", len(b))
			}
			return 5, nil
		})
		// Read actual data
		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			return copy(b, "data"), nil
		})

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 5)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if string(d) != "data" {
			t.Errorf("Expected 'data', got %s", d)
		}
		// skip offset since it's not exported
	})

	t.Run("Fallback_Skip_EOF", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		// Skip returns EOF immediately
		m.EXPECT().Read(gomock.Any()).Return(0, io.EOF)

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 5)
		if errno != 0 {
			t.Errorf("expected success (0 errno) on EOF, got %v", errno)
		}
		d, _ := res.Bytes(dest)
		if len(d) != 0 {
			t.Errorf("expected 0 bytes, got %d", len(d))
		}
	})

	t.Run("Fallback_Skip_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Read(gomock.Any()).Return(0, errors.New("skip fail"))

		_, errno := fh.Read(ctx, make([]byte, 10), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Read_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Read(gomock.Any()).Return(0, errors.New("read fail"))

		_, errno := fh.Read(ctx, make([]byte, 10), 0)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Read_EOF", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			copy(b, "part")
			return 4, io.EOF
		})

		dest := make([]byte, 10)
		res, errno := fh.Read(ctx, dest, 0)
		if errno != 0 {
			t.Errorf("Read failed: %v", errno)
		}
		d, _ := res.Bytes(dest)
		if string(d) != "part" {
			t.Errorf("Expected 'part', got %s", d)
		}
	})

	t.Run("Fallback_Backwards", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			return 10, nil
		})
		fh := MakeFileHandle(t, ctrl, m)
		// force offset to 10
		_, _ = fh.Read(t.Context(), make([]byte, 10), 0)

		_, errno := fh.Read(ctx, make([]byte, 10), 5)
		if errno != syscall.ENOSYS {
			t.Errorf("expected ENOSYS, got %v", errno)
		}
	})
}

func TestFileHandle_Write(t *testing.T) {
	t.Run("WriterAt_Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().WriteAt(gomock.Any(), int64(0)).Return(4, nil)

		n, errno := fh.Write(ctx, []byte("data"), 0)
		if errno != 0 {
			t.Errorf("Write failed: %v", errno)
		}
		if n != 4 {
			t.Errorf("Expected 4 bytes, got %d", n)
		}
	})

	t.Run("WriterAt_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)
		m.EXPECT().WriteAt(gomock.Any(), int64(0)).Return(0, errors.New("fail"))

		_, errno := fh.Write(ctx, []byte("data"), 0)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Seeker_Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().WriteAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(5), nil)
		m.EXPECT().Write(gomock.Any()).Return(3, nil)

		n, errno := fh.Write(ctx, []byte("msg"), 5)
		if errno != 0 {
			t.Errorf("Write failed: %v", errno)
		}
		if n != 3 {
			t.Errorf("Expected 3 bytes, got %d", n)
		}
	})

	t.Run("Seeker_Seek_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().WriteAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(0), errors.New("seek fail"))

		_, errno := fh.Write(ctx, []byte("msg"), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Seeker_Write_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mock.NewMockFullFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().WriteAt(gomock.Any(), int64(5)).Return(0, errors.ErrUnsupported)
		m.EXPECT().Seek(int64(5), 0).Return(int64(5), nil)
		m.EXPECT().Write(gomock.Any()).Return(0, errors.New("write fail"))

		_, errno := fh.Write(ctx, []byte("msg"), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Success_Sequential", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Write(gomock.Any()).Return(4, nil)

		n, errno := fh.Write(ctx, []byte("data"), 0)
		if errno != 0 {
			t.Errorf("Write failed: %v", errno)
		}
		if n != 4 {
			t.Errorf("Expected 4 bytes, got %d", n)
		}
		// skip offset since it's not exported
	})

	t.Run("Fallback_Success_Pad", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		// Pad 5 bytes with zeros
		m.EXPECT().Write(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			if len(b) != 5 {
				t.Errorf("Pad write length mismatch: got %d", len(b))
			}
			for i, v := range b {
				if v != 0 {
					t.Errorf("Non-zero padding byte at %d: %v", i, v)
				}
			}
			return 5, nil
		})
		// Write actual data
		m.EXPECT().Write(gomock.Any()).Return(4, nil)

		n, errno := fh.Write(ctx, []byte("data"), 5)
		if errno != 0 {
			t.Errorf("Write failed: %v", errno)
		}
		if n != 4 {
			t.Errorf("Expected 4 bytes, got %d", n)
		}
		// skip offset since it's not exported
	})

	t.Run("Fallback_Pad_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Write(gomock.Any()).Return(0, errors.New("pad fail"))

		_, errno := fh.Write(ctx, []byte("data"), 5)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Write_Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		fh := MakeFileHandle(t, ctrl, m)

		m.EXPECT().Write(gomock.Any()).Return(0, errors.New("write fail"))

		_, errno := fh.Write(ctx, []byte("data"), 0)
		if errno != syscall.EIO {
			t.Errorf("expected EIO, got %v", errno)
		}
	})

	t.Run("Fallback_Backwards", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		ctx := t.Context()
		m := mockfs.NewMockFile(ctrl)
		m.EXPECT().Read(gomock.Any()).DoAndReturn(func(b []byte) (int, error) {
			return 10, nil
		})
		fh := MakeFileHandle(t, ctrl, m)
		// force offset to 10
		_, _ = fh.Read(t.Context(), make([]byte, 10), 0)

		_, errno := fh.Write(ctx, []byte("data"), 5)
		if errno != syscall.ENOSYS {
			t.Errorf("expected ENOSYS, got %v", errno)
		}
	})
}

func TestFileHandle_Flush_Release(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx := t.Context()

	mf := mockfs.NewMockFile(ctrl)
	fh := MakeFileHandle(t, ctrl, mf)

	if errno := fh.Flush(ctx); errno != 0 {
		t.Errorf("Flush failed: %v", errno)
	}

	mf.EXPECT().Close().Return(nil)
	if errno := fh.Release(ctx); errno != 0 {
		t.Errorf("Release failed: %v", errno)
	}
}
