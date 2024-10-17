package osutil

import (
	"os"
	"strconv"

	"github.com/docker/docker/pkg/ioutils"
	"github.com/pkg/errors"
)

func MkdirAll(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	if suid, sgid, ok := sudoOwner(); ok {
		return os.Chown(path, suid, sgid)
	}
	return nil
}

func MkdirTemp(dir, pattern string) (string, error) {
	tmpdir, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	if suid, sgid, ok := sudoOwner(); ok {
		return tmpdir, os.Chown(tmpdir, suid, sgid)
	}
	return tmpdir, nil
}

func WriteFile(filename string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(filename, data, perm); err != nil {
		return err
	}
	if suid, sgid, ok := sudoOwner(); ok {
		return os.Chown(filename, suid, sgid)
	}
	return nil
}

func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	if err := ioutils.AtomicWriteFile(filename, data, perm); err != nil {
		return err
	}
	if suid, sgid, ok := sudoOwner(); ok {
		return os.Chown(filename, suid, sgid)
	}
	return nil
}

func Create(name string) (*os.File, error) {
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	if suid, sgid, ok := sudoOwner(); ok {
		if err := f.Chown(suid, sgid); err != nil {
			return nil, errors.Wrapf(err, "failed to chown %s", name)
		}
	}
	return f, nil
}

func sudoOwner() (int, int, bool) {
	if _, ok := os.LookupEnv("SUDO_COMMAND"); !ok {
		return -1, -1, false
	}
	sudoUID := os.Getenv("SUDO_UID") // https://www.sudo.ws/docs/man/sudo.man/#SUDO_UID
	sudoGID := os.Getenv("SUDO_GID") // https://www.sudo.ws/docs/man/sudo.man/#SUDO_GID
	if sudoUID == "" || sudoGID == "" {
		return -1, -1, false
	}
	suid, err := strconv.Atoi(sudoUID)
	if err != nil {
		return -1, -1, false
	}
	sgid, err := strconv.Atoi(sudoGID)
	if err != nil {
		return -1, -1, false
	}
	return suid, sgid, true
}
