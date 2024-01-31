package osutil

import "os"

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
