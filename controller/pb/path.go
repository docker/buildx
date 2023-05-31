package pb

import (
	"path/filepath"
	"strings"

	"github.com/docker/docker/builder/remotecontext/urlutil"
	"github.com/moby/buildkit/util/gitutil"
)

// ResolveOptionPaths resolves all paths contained in BuildOptions
// and replaces them to absolute paths.
func ResolveOptionPaths(options *BuildOptions) (_ *BuildOptions, err error) {
	var cacheFrom []*CacheOptionsEntry
	for _, co := range options.CacheFrom {
		switch co.Type {
		case "local":
			var attrs map[string]string
			for k, v := range co.Attrs {
				if attrs == nil {
					attrs = make(map[string]string)
				}
				switch k {
				case "src":
					p := v
					if p != "" {
						p, err = filepath.Abs(p)
						if err != nil {
							return nil, err
						}
					}
					attrs[k] = p
				default:
					attrs[k] = v
				}
			}
			co.Attrs = attrs
			cacheFrom = append(cacheFrom, co)
		default:
			cacheFrom = append(cacheFrom, co)
		}
	}
	options.CacheFrom = cacheFrom

	var cacheTo []*CacheOptionsEntry
	for _, co := range options.CacheTo {
		switch co.Type {
		case "local":
			var attrs map[string]string
			for k, v := range co.Attrs {
				if attrs == nil {
					attrs = make(map[string]string)
				}
				switch k {
				case "dest":
					p := v
					if p != "" {
						p, err = filepath.Abs(p)
						if err != nil {
							return nil, err
						}
					}
					attrs[k] = p
				default:
					attrs[k] = v
				}
			}
			co.Attrs = attrs
			cacheTo = append(cacheTo, co)
		default:
			cacheTo = append(cacheTo, co)
		}
	}
	options.CacheTo = cacheTo
	var exports []*ExportEntry
	for _, e := range options.Exports {
		if e.Destination != "" && e.Destination != "-" {
			e.Destination, err = filepath.Abs(e.Destination)
			if err != nil {
				return nil, err
			}
		}
		exports = append(exports, e)
	}
	options.Exports = exports

	var secrets []*Secret
	for _, s := range options.Secrets {
		if s.FilePath != "" {
			s.FilePath, err = filepath.Abs(s.FilePath)
			if err != nil {
				return nil, err
			}
		}
		secrets = append(secrets, s)
	}
	options.Secrets = secrets

	var ssh []*SSH
	for _, s := range options.SSH {
		var ps []string
		for _, pt := range s.Paths {
			p := pt
			if p != "" {
				p, err = filepath.Abs(p)
				if err != nil {
					return nil, err
				}
			}
			ps = append(ps, p)

		}
		s.Paths = ps
		ssh = append(ssh, s)
	}
	options.SSH = ssh

	if options.Inputs == nil {
		return options, nil
	}

	localContext := false
	if options.Inputs.ContextPath != "" && options.Inputs.ContextPath != "-" {
		if !isRemoteURL(options.Inputs.ContextPath) {
			localContext = true
			options.Inputs.ContextPath, err = filepath.Abs(options.Inputs.ContextPath)
			if err != nil {
				return nil, err
			}
		}
	}
	if options.Inputs.DockerfileName != "" && options.Inputs.DockerfileName != "-" {
		if localContext && !urlutil.IsURL(options.Inputs.DockerfileName) {
			options.Inputs.DockerfileName, err = filepath.Abs(options.Inputs.DockerfileName)
			if err != nil {
				return nil, err
			}
		}
	}

	var contexts map[string]*NamedContext
	for k, v := range options.Inputs.NamedContexts {
		v := *v
		if v.Definition != nil {
			// definition, no path
		} else if urlutil.IsGitURL(v.Path) || urlutil.IsURL(v.Path) || strings.HasPrefix(v.Path, "docker-image://") {
			// url prefix, this is a remote path
		} else if strings.HasPrefix(v.Path, "oci-layout://") {
			// oci layout prefix, this is a local path
			p := strings.TrimPrefix(v.Path, "oci-layout://")
			p, err = filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			v.Path = "oci-layout://" + p
		} else {
			// no prefix, assume local path
			v.Path, err = filepath.Abs(v.Path)
			if err != nil {
				return nil, err
			}
		}

		if contexts == nil {
			contexts = make(map[string]*NamedContext)
		}
		contexts[k] = &v
	}
	options.Inputs.NamedContexts = contexts

	return options, nil
}

func isRemoteURL(c string) bool {
	if urlutil.IsURL(c) {
		return true
	}
	if _, err := gitutil.ParseGitRef(c); err == nil {
		return true
	}
	return false
}
