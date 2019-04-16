package build

import (
	"archive/tar"
	"bytes"
	"os"
)

// archiveHeaderSize is the number of bytes in an archive header
const archiveHeaderSize = 512

func isLocalDir(c string) bool {
	st, err := os.Stat(c)
	return err == nil && st.IsDir()
}

func isArchive(header []byte) bool {
	for _, m := range [][]byte{
		{0x42, 0x5A, 0x68},                   // bzip2
		{0x1F, 0x8B, 0x08},                   // gzip
		{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}, // xz
	} {
		if len(header) < len(m) {
			continue
		}
		if bytes.Equal(m, header[:len(m)]) {
			return true
		}
	}

	r := tar.NewReader(bytes.NewBuffer(header))
	_, err := r.Next()
	return err == nil
}
