package osutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// GetWd retrieves the current working directory.
//
// On Windows, this function will return the long path name
// version of the path.
func GetWd() string {
	wd, _ := os.Getwd()
	if lp, err := GetLongPathName(wd); err == nil {
		return lp
	}
	return wd
}

func IsLocalDir(c string) bool {
	st, err := os.Stat(c)
	return err == nil && st.IsDir()
}

func ToAbs(path string) string {
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(filepath.Join(GetWd(), path))
	}
	return SanitizePath(path)
}

func EvaluateToExistingPath(in string) (string, string, error) {
	in, err := filepath.Abs(in)
	if err != nil {
		return "", "", err
	}

	volLen := volumeNameLen(in)
	pathSeparator := string(os.PathSeparator)

	if volLen < len(in) && os.IsPathSeparator(in[volLen]) {
		volLen++
	}
	vol := in[:volLen]
	dest := vol
	linksWalked := 0
	var end int
	for start := volLen; start < len(in); start = end {
		for start < len(in) && os.IsPathSeparator(in[start]) {
			start++
		}
		end = start
		for end < len(in) && !os.IsPathSeparator(in[end]) {
			end++
		}

		if end == start {
			break
		} else if in[start:end] == "." {
			continue
		} else if in[start:end] == ".." {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen || dest[r+1:] == ".." {
				if len(dest) > volLen {
					dest += pathSeparator
				}
				dest += ".."
			} else {
				dest = dest[:r]
			}
			continue
		}

		if len(dest) > volumeNameLen(dest) && !os.IsPathSeparator(dest[len(dest)-1]) {
			dest += pathSeparator
		}
		dest += in[start:end]

		fi, err := os.Lstat(dest)
		if err != nil {
			if os.IsNotExist(err) {
				for r := len(dest) - 1; r >= volLen; r-- {
					if os.IsPathSeparator(dest[r]) {
						return dest[:r], in[start:], nil
					}
				}
				return vol, in[start:], nil
			}
			return "", "", err
		}

		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.Mode().IsDir() && end < len(in) {
				return "", "", syscall.ENOTDIR
			}
			continue
		}

		linksWalked++
		if linksWalked > 255 {
			return "", "", errors.New("too many symlinks")
		}

		link, err := os.Readlink(dest)
		if err != nil {
			return "", "", err
		}

		in = link + in[end:]

		v := volumeNameLen(link)
		if v > 0 {
			if v < len(link) && os.IsPathSeparator(link[v]) {
				v++
			}
			vol = link[:v]
			dest = vol
			end = len(vol)
		} else if len(link) > 0 && os.IsPathSeparator(link[0]) {
			dest = link[:1]
			end = 1
			vol = link[:1]
			volLen = 1
		} else {
			var r int
			for r = len(dest) - 1; r >= volLen; r-- {
				if os.IsPathSeparator(dest[r]) {
					break
				}
			}
			if r < volLen {
				dest = vol
			} else {
				dest = dest[:r]
			}
			end = 0
		}
	}
	return filepath.Clean(dest), "", nil
}

func volumeNameLen(s string) int {
	return len(filepath.VolumeName(s))
}
