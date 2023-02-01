package bake

import (
	"archive/tar"
	"bytes"
	"context"
	"strings"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
)

type Input struct {
	State *llb.State
	URL   string
}

func ReadRemoteFiles(ctx context.Context, nodes []builder.Node, url string, names []string, pw progress.Writer) ([]File, *Input, error) {
	var filename string
	st, ok := detectGitContext(url)
	if !ok {
		st, filename, ok = detectHTTPContext(url)
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
	_, err = c.Build(ctx, client.SolveOpt{}, "buildx", func(ctx context.Context, c gwclient.Client) (*gwclient.Result, error) {
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

func IsRemoteURL(url string) bool {
	if _, _, ok := detectHTTPContext(url); ok {
		return true
	}
	if _, ok := detectGitContext(url); ok {
		return true
	}
	return false
}

func detectHTTPContext(url string) (*llb.State, string, bool) {
	if httpPrefix.MatchString(url) {
		httpContext := llb.HTTP(url, llb.Filename("context"), llb.WithCustomName("[internal] load remote build context"))
		return &httpContext, "context", true
	}
	return nil, "", false
}

func detectGitContext(ref string) (*llb.State, bool) {
	found := false
	if httpPrefix.MatchString(ref) && gitURLPathWithFragmentSuffix.MatchString(ref) {
		found = true
	}

	for _, prefix := range []string{"git://", "github.com/", "git@"} {
		if strings.HasPrefix(ref, prefix) {
			found = true
			break
		}
	}
	if !found {
		return nil, false
	}

	parts := strings.SplitN(ref, "#", 2)
	branch := ""
	if len(parts) > 1 {
		branch = parts[1]
	}
	gitOpts := []llb.GitOption{llb.WithCustomName("[internal] load git source " + ref)}

	st := llb.Git(parts[0], branch, gitOpts...)
	return &st, true
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
		_, err := ref.StatFile(ctx, gwclient.StatRequest{Path: name})
		if err != nil {
			if isDefault {
				continue
			}
			return nil, err
		}
		dt, err := ref.ReadFile(ctx, gwclient.ReadRequest{Filename: name})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Name: name, Data: dt})
	}

	return files, nil
}
