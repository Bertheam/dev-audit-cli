package discovery

import (
	"io/fs"
	"os"
	"path/filepath"
)

type OSFileSystem struct{}

func (OSFileSystem) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(name)
}

func (OSFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}
