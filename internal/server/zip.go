package server

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

// ZipDirectory streams a zip of srcDir to w.
func ZipDirectory(w io.Writer, srcDir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = rel
		fh.Method = zip.Deflate
		f, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(f, file)
		return err
	})
}
