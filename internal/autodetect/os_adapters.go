package autodetect

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type osFileSystem struct{}

func (osFileSystem) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(name)
}

func (osFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(name)
}

func (osFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}

func (osFileSystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

type osEnvironment struct{}

func (osEnvironment) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func (osEnvironment) Getwd() (string, error) {
	return os.Getwd()
}

func (osEnvironment) LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

func (osEnvironment) LookPath(file string) (string, error) {
	pathValue, ok := os.LookupEnv("PATH")
	if !ok {
		return "", errors.New("PATH is not set")
	}
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, file)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
