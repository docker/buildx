package history

import (
	"path/filepath"
	"strings"

	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/urlutil"
	"github.com/moby/buildkit/frontend/dockerfile/dfgitutil"
)

func BuildName(fattrs map[string]string, ls *localstate.State) string {
	if v, ok := fattrs["build-arg:BUILDKIT_BUILD_NAME"]; ok && v != "" {
		return v
	}

	var res string

	var target, contextPath, dockerfilePath, vcsSource string
	if v, ok := fattrs["target"]; ok {
		target = v
	}
	if v, ok := fattrs["context"]; ok {
		contextPath = filepath.ToSlash(v)
	} else if v, ok := fattrs["vcs:localdir:context"]; ok && v != "." {
		contextPath = filepath.ToSlash(v)
	}
	if v, ok := fattrs["vcs:source"]; ok {
		vcsSource = v
	} else if v, ok := fattrs["input:context"]; ok {
		if _, ok, _ := dfgitutil.ParseGitRef(v); ok {
			vcsSource = v
		}
	}
	if v, ok := fattrs["filename"]; ok && v != "Dockerfile" {
		dockerfilePath = filepath.ToSlash(v)
	}
	if v, ok := fattrs["vcs:localdir:dockerfile"]; ok && v != "." {
		dockerfilePath = filepath.ToSlash(filepath.Join(v, dockerfilePath))
	}

	var localPath string
	if ls != nil && !urlutil.IsRemoteURL(ls.LocalPath) {
		if ls.LocalPath != "" && ls.LocalPath != "-" {
			localPath = filepath.ToSlash(ls.LocalPath)
		}
		if ls.DockerfilePath != "" && ls.DockerfilePath != "-" && ls.DockerfilePath != "Dockerfile" {
			dockerfilePath = filepath.ToSlash(ls.DockerfilePath)
		}
	}

	const defaultFilename = "/Dockerfile"
	hasDefaultFileName := strings.HasSuffix(dockerfilePath, defaultFilename) || dockerfilePath == ""
	dockerfilePath = strings.TrimSuffix(dockerfilePath, defaultFilename)

	if strings.HasPrefix(dockerfilePath, localPath) && len(dockerfilePath) > len(localPath) {
		res = dockerfilePath[strings.LastIndex(localPath, "/")+1:]
	} else {
		bpath := localPath
		if len(dockerfilePath) > 0 {
			bpath = dockerfilePath
		}
		if len(bpath) > 0 {
			lidx := strings.LastIndex(bpath, "/")
			res = bpath[lidx+1:]
			if !hasDefaultFileName {
				if lidx != -1 {
					res = filepath.ToSlash(filepath.Join(filepath.Base(bpath[:lidx]), res))
				} else {
					res = filepath.ToSlash(filepath.Join(filepath.Base(bpath), res))
				}
			}
		}
	}

	if len(contextPath) > 0 {
		res = contextPath
	}
	if len(target) > 0 {
		if len(res) > 0 {
			res = res + " (" + target + ")"
		} else {
			res = target
		}
	}
	if res == "" && vcsSource != "" {
		u, _ := dfgitutil.FragmentFormat(vcsSource)
		return u
	}
	return res
}
