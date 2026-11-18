package runner

import (
	"archive/tar"
	"bytes"
	"time"
)

// tarOf packs files into an uncompressed tar, for `tar -xf -` on the
// runner.
func tarOf(files []file) ([]byte, error) {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	now := time.Now()
	for _, f := range files {
		if err := w.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
