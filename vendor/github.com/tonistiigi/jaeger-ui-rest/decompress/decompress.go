package decompress

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
)

type decompressFS struct {
	fs.FS
	mu     sync.Mutex
	data   map[string][]byte
	inject Injector
}

type Injector interface {
	Inject(name string, dt []byte) ([]byte, bool)
}

func NewFS(fsys fs.FS, injector Injector) fs.FS {
	return &decompressFS{
		FS:     fsys,
		data:   make(map[string][]byte),
		inject: injector,
	}
}

func (d *decompressFS) Open(name string) (fs.File, error) {
	name = filepath.Clean(name)

	f, err := d.FS.Open(name)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	dt, ok := d.data[name]
	if ok {
		return &staticFile{
			Reader: bytes.NewReader(dt),
			f:      f,
		}, nil
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if fi.IsDir() {
		return f, nil
	}

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, gzReader); err != nil {
		f.Close()
		return nil, err
	}

	dt = buf.Bytes()
	if d.inject != nil {
		newdt, ok := d.inject.Inject(name, dt)
		if ok {
			dt = newdt
		}
	}

	d.data[name] = dt

	return &staticFile{
		Reader: bytes.NewReader(dt),
		f:      f,
	}, nil
}

type staticFile struct {
	*bytes.Reader
	f fs.File
}

func (s *staticFile) Stat() (fs.FileInfo, error) {
	fi, err := s.f.Stat()
	if err != nil {
		return nil, err
	}
	return &fileInfo{
		FileInfo: fi,
		size:     int64(s.Len()),
	}, nil
}

func (s *staticFile) Close() error {
	return s.f.Close()
}

type fileInfo struct {
	fs.FileInfo
	size int64
}

func (f *fileInfo) Size() int64 {
	return f.size
}

var _ fs.File = &staticFile{}
