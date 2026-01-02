package fsfuse_test

import (
	"io/fs"
	"time"

	"github.com/gwangyi/fsx/mockfs"
	"go.uber.org/mock/gomock"
)

func setupFileInfo(ctrl *gomock.Controller, name string, size int64, mode fs.FileMode) *mockfs.MockFileInfo {
	mfi := mockfs.NewMockFileInfo(ctrl)
	mfi.EXPECT().Name().Return(name).AnyTimes()
	mfi.EXPECT().Size().Return(size).AnyTimes()
	mfi.EXPECT().Mode().Return(mode).AnyTimes()
	mfi.EXPECT().ModTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().IsDir().Return(mode.IsDir()).AnyTimes()
	mfi.EXPECT().Sys().Return(nil).AnyTimes()
	mfi.EXPECT().AccessTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().ChangeTime().Return(time.Now()).AnyTimes()
	mfi.EXPECT().Owner().Return("1000").AnyTimes()
	mfi.EXPECT().Group().Return("1000").AnyTimes()
	return mfi
}
