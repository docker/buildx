package bake

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"

	"github.com/compose-spec/compose-go/loader"
	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/buildx/builder"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/dockerui"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session"
	"github.com/pkg/errors"
)

type Input struct {
	State *llb.State
	URL   string
}

func ReadRemoteFiles(ctx context.Context, nodes []builder.Node, url string, names []string, pw progress.Writer) ([]File, *Input, error) {
	var session []session.Attachable
	var filename string
	st, ok := dockerui.DetectGitContext(url, false)
	if ok {
		ssh, err := controllerapi.CreateSSH([]*controllerapi.SSH{{ID: "default"}})
		if err == nil {
			session = append(session, ssh)
		}
	} else {
		st, filename, ok = dockerui.DetectHTTPContext(url)
		if !ok {
			return nil, nil, errors.Errorf("not url context")
		}
	}

	inp := &Input{State: st, URL: url}
	var files []File

	var node *builder.Node
	for i, n := range nodes {
		if n.Err == nil {
			node = &nodes[i]
			continue
		}
	}
	if node == nil {
		return nil, nil, nil
	}

	c, err := driver.Boot(ctx, ctx, node.Driver, pw)
	if err != nil {
		return nil, nil, err
	}

	ch, done := progress.NewChannel(pw)
	defer func() { <-done }()
	_, err = c.Build(ctx, client.SolveOpt{Session: session, Internal: true}, "buildx", func(ctx context.Context, c gwclient.Client) (*gwclient.Result, error) {
		def, err := st.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		res, err := c.Solve(ctx, gwclient.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}

		ref, err := res.SingleRef()
		if err != nil {
			return nil, err
		}

		if filename != "" {
			files, err = filesFromURLRef(ctx, c, ref, inp, filename, names)
		} else {
			files, err = filesFromRef(ctx, ref, names)
		}
		return nil, err
	}, ch)

	if err != nil {
		return nil, nil, err
	}

	return files, inp, nil
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

func filesFromURLRef(ctx context.Context, c gwclient.Client, ref gwclient.Reference, inp *Input, filename string, names []string) ([]File, error) {
	stat, err := ref.StatFile(ctx, gwclient.StatRequest{Path: filename})
	if err != nil {
		return nil, err
	}

	dt, err := ref.ReadFile(ctx, gwclient.ReadRequest{
		Filename: filename,
		Range: &gwclient.FileRange{
			Length: 1024,
		},
	})
	if err != nil {
		return nil, err
	}

	if isArchive(dt) {
		bc := llb.Scratch().File(llb.Copy(inp.State, filename, "/", &llb.CopyInfo{
			AttemptUnpack: true,
		}))
		inp.State = &bc
		inp.URL = ""
		def, err := bc.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		res, err := c.Solve(ctx, gwclient.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}

		ref, err := res.SingleRef()
		if err != nil {
			return nil, err
		}

		return filesFromRef(ctx, ref, names)
	}

	inp.State = nil
	name := inp.URL
	inp.URL = ""

	if len(dt) > stat.Size() {
		if stat.Size() > 1024*512 {
			return nil, errors.Errorf("non-archive definition URL bigger than maximum allowed size")
		}

		dt, err = ref.ReadFile(ctx, gwclient.ReadRequest{
			Filename: filename,
		})
		if err != nil {
			return nil, err
		}
	}

	return []File{{Name: name, Data: dt}}, nil
}

func filesFromRef(ctx context.Context, ref gwclient.Reference, names []string) ([]File, error) {
	// TODO: auto-remove parent dir in needed
	var files []File

	isDefault := false
	if len(names) == 0 {
		isDefault = true
		names = defaultFilenames()
	}

	for _, name := range names {
		f, err := readRefFile(name, func(name string) (*File, error) {
			_, err := ref.StatFile(ctx, gwclient.StatRequest{Path: name})
			if err != nil {
				if isDefault {
					return nil, nil
				}
				return nil, err
			}
			dt, err := ref.ReadFile(ctx, gwclient.ReadRequest{Filename: name})
			if err != nil {
				return nil, err
			}
			return &File{Name: name, Data: dt}, nil
		}, nil)
		if err != nil {
			return nil, err
		} else if len(f) == 0 {
			continue
		}
		files = append(files, f...)
	}

	return files, nil
}

func readRefFile(name string, readFunc func(name string) (*File, error), files []File) ([]File, error) {
	f, err := readFunc(name)
	if err != nil {
		return nil, err
	} else if f == nil {
		return files, nil
	}
	// if we have a compose file, we need to read it and extract the
	// include files from it
	_, err = loader.Load(compose.ConfigDetails{
		ConfigFiles: []compose.ConfigFile{
			{
				Content: f.Data,
			},
		},
		Environment: sliceToMap(os.Environ()),
	}, func(options *loader.Options) {
		options.SetProjectName("bake", false)
		options.SkipNormalization = true
		options.SkipConsistencyCheck = true
		options.SkipInclude = true
	})
	if err == nil {
		yml, err := loader.ParseYAML(f.Data)
		if err != nil {
			return nil, err
		}
		if v, ok := yml["include"]; ok && v != "" {
			// include files will be read later and added to the list of files
			// to be processed, so we can remote "include" from the yaml file
			delete(yml, "include")
			// marshal the yaml file again without the include key and add it
			// to the list of files to be processed
			newData, err := json.Marshal(yml)
			if err != nil {
				return nil, err
			}
			files = append(files, File{
				Name: f.Name,
				Data: newData,
			})
			// read include files
			if idt, ok := v.([]interface{}); ok && len(idt) > 0 {
				var includes []string
				for _, i := range idt {
					includes = append(includes, i.(string))
				}
				for _, include := range includes {
					incf, err := readRefFile(include, readFunc, files)
					if err != nil {
						return nil, err
					} else if incf == nil {
						continue
					}
					files = append(files, incf...)
				}
			}
		}
	} else {
		files = append(files, *f)
	}
	return files, nil
}
