module github.com/gwangyi/fsfuse

go 1.25.5

require (
	github.com/gwangyi/fsx v0.0.0-20251211152421-6790f57f84c1
	github.com/hanwen/go-fuse/v2 v2.9.0
	go.uber.org/mock v0.6.0
)

require golang.org/x/sys v0.28.0 // indirect

replace github.com/gwangyi/fsx => /home/gwangyi/workspace/fsx
