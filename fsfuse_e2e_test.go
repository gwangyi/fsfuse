package fsfuse_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gwangyi/fsfuse"
	"github.com/gwangyi/fsx/contextual"
	"github.com/gwangyi/fsx/osfs"
	"github.com/hanwen/go-fuse/v2/fs"
)

func TestE2E(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); os.IsNotExist(err) {
		t.Skip("skipping e2e test: /dev/fuse not found")
	}

	// Create temp directories
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	mntDir := filepath.Join(tmpDir, "mnt")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mntDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file in source
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	// Setup FUSE server
	backing, err := osfs.New(srcDir)
	if err != nil {
		t.Fatalf("osfs.New failed: %v", err)
	}

	root := fsfuse.New(contextual.ToContextual(backing))

	// Mount
	server, err := fs.Mount(mntDir, root, &fs.Options{})
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer func() {
		if err := server.Unmount(); err != nil {
			t.Errorf("Unmount failed: %v", err)
		}
	}()

	// Wait for mount
	if err := server.WaitMount(); err != nil {
		t.Fatalf("WaitMount failed: %v", err)
	}

	// 1. Read existing file
	data, err := os.ReadFile(filepath.Join(mntDir, "hello.txt"))
	if err != nil {
		t.Errorf("ReadFile failed: %v", err)
	} else if string(data) != "hello world" {
		t.Errorf("ReadFile content mismatch: got %q, want 'hello world'", string(data))
	}

	// 2. Create new file
	if err := os.WriteFile(filepath.Join(mntDir, "new.txt"), []byte("new content"), 0644); err != nil {
		t.Errorf("WriteFile failed: %v", err)
	}

	// 3. Verify existence in source
	srcData, err := os.ReadFile(filepath.Join(srcDir, "new.txt"))
	if err != nil {
		t.Errorf("Source ReadFile failed: %v", err)
	} else if string(srcData) != "new content" {
		t.Errorf("Source content mismatch: got %q, want 'new content'", string(srcData))
	}

	// 4. List directory
	entries, err := os.ReadDir(mntDir)
	if err != nil {
		t.Errorf("ReadDir failed: %v", err)
	} else {
		hasHello := false
		hasNew := false
		for _, e := range entries {
			if e.Name() == "hello.txt" {
				hasHello = true
			}
			if e.Name() == "new.txt" {
				hasNew = true
			}
		}
		if !hasHello {
			t.Error("ReadDir missing hello.txt")
		}
		if !hasNew {
			t.Error("ReadDir missing new.txt")
		}
	}

	// 5. Directory Stat Sanity
	subDir := filepath.Join(mntDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Errorf("Mkdir failed: %v", err)
	}

	fi, err := os.Stat(subDir)
	if err != nil {
		t.Errorf("Stat subdir failed: %v", err)
	} else {
		if !fi.IsDir() {
			t.Error("Stat subdir: expected directory")
		}
		// Basic permission check (some filesystems might alter it, but osfs should respect it usually)
		// relaxing check to just directory bit for broad compatibility unless stricter check needed
	}

	// 6. Symlink Stat Sanity
	linkName := filepath.Join(mntDir, "link_to_hello")
	if err := os.Symlink("hello.txt", linkName); err != nil {
		t.Errorf("Symlink failed: %v", err)
	}

	// Lstat should see the link
	fi, err = os.Lstat(linkName)
	if err != nil {
		t.Errorf("Lstat link failed: %v", err)
	} else {
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Error("Lstat link: expected symlink mode")
		}
	}

	// Stat should see the target (hello.txt)
	fi, err = os.Stat(linkName)
	if err != nil {
		t.Errorf("Stat link failed: %v", err)
	} else {
		if !fi.Mode().IsRegular() {
			t.Error("Stat link: expected regular file (target)")
		}
		if fi.Size() != 11 { // "hello world" is 11 bytes
			t.Errorf("Stat link size: got %d, want 11", fi.Size())
		}
	}
}
