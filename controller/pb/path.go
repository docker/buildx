package pb

import (
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/util/gitutil"
)

// ResolveOptionPaths resolves all paths contained in BuildOptions
// and replaces them to absolute paths.
func ResolveOptionPaths(options *BuildOptions) (_ *BuildOptions, err error) {
	localContext := false
	if options.ContextPath != "" && options.ContextPath != "-" {
		if !isRemoteURL(options.ContextPath) {
			localContext = true
			options.ContextPath, err = filepath.Abs(options.ContextPath)
			if err != nil {
				return nil, err
			}
		}
	}
	if options.DockerfileName != "" && options.DockerfileName != "-" {
		if localContext && !isHTTPURL(options.DockerfileName) {
			options.DockerfileName, err = filepath.Abs(options.DockerfileName)
			if err != nil {
				return nil, err
			}
		}
	}

	var contexts map[string]string
	for k, v := range options.NamedContexts {
		if isRemoteURL(v) || strings.HasPrefix(v, "docker-image://") {
			// url prefix, this is a remote path
		} else if strings.HasPrefix(v, "oci-layout://") {
			// oci layout prefix, this is a local path
			p := strings.TrimPrefix(v, "oci-layout://")
			p, err = filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			v = "oci-layout://" + p
		} else {
			// no prefix, assume local path
			v, err = filepath.Abs(v)
			if err != nil {
				return nil, err
			}
		}

		if contexts == nil {
			contexts = make(map[string]string)
		}
		contexts[k] = v
	}
	options.NamedContexts = contexts

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

	return options, nil
}

// isHTTPURL returns true if the provided str is an HTTP(S) URL by checking if it
// has a http:// or https:// scheme. No validation is performed to verify if the
// URL is well-formed.
func isHTTPURL(str string) bool {
	return strings.HasPrefix(str, "https://") || strings.HasPrefix(str, "http://")
}

func isRemoteURL(c string) bool {
	if isHTTPURL(c) {
		return true
	}
	if _, err := gitutil.ParseGitRef(c); err == nil {
		return true
	}
	return false
}
