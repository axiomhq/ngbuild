package core

import (
	"os"
	"path/filepath"
)

// Exists returns whether the given file or directory exists or not
func Exists(paths ...string) (bool, error) {
	path := filepath.Join(paths...)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
