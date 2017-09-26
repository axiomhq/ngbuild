package core

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// writeAll will write the entire buffer to file or die trying
// i have no idea why go doesn't have this, it has ReadAll
func writeAll(f io.Writer, buf []byte) error {
	for len(buf) > 0 {
		n, err := f.Write(buf)
		if err != nil {
			return err
		}
		buf = buf[n:]
	}

	return nil
}

// CopyFile doesn't care if the file already exists, it just overwrites it.
// It does however, try to be at least a little bit atomic
func CopyFile(src, dst string) error {
	_, name := filepath.Split(src)
	tmpFile, err := ioutil.TempFile(os.TempDir(), name)
	defer tmpFile.Close() //nolint (errcheck)
	if err != nil {
		return err
	}

	tmpFileName := tmpFile.Name()
	defer os.Remove(tmpFileName) //nolint (errcheck)

	srcFile, err := os.Open(src)
	defer srcFile.Close() //nolint (errcheck)
	if err != nil {
		return err
	}

	for err == nil {
		var n int
		var buf [4 * 1024]byte
		n, err = srcFile.Read(buf[:])
		if err != nil && err != io.EOF {
			return err
		}

		writeAll(tmpFile, buf[:n]) //nolint (errcheck)
	}

	// if we get here, tmpFile has a copy of src
	tmpFile.Close() //nolint (errcheck)
	srcFile.Close() //nolint (errcheck)

	return os.Rename(tmpFileName, dst)
}
