package storeutil

import (
	"context"
	"io"
	"sync"

	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/docker"
	"github.com/docker/docker/client"
)

// DockerClient represents an active docker object.
type DockerClient struct {
	cli command.Cli
}

// NewDockerClient initializes a new docker client.
func NewDockerClient(cli command.Cli) *DockerClient {
	return &DockerClient{cli: cli}
}

// API returns a new docker API client.
func (c *DockerClient) API(name string) (client.APIClient, error) {
	if name == "" {
		name = c.cli.CurrentContext()
	}
	return ClientForEndpoint(c.cli, name)
}

// LoadImage imports an image to docker.
func (c *DockerClient) LoadImage(ctx context.Context, name string, status progress.Writer) (io.WriteCloser, func(), error) {
	dapi, err := c.API(name)
	if err != nil {
		return nil, nil, err
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})

	ctx, cancel := context.WithCancel(ctx)
	var w *waitingWriter
	w = &waitingWriter{
		PipeWriter: pw,
		f: func() {
			resp, err := dapi.ImageLoad(ctx, pr, false)
			defer close(done)
			if err != nil {
				pr.CloseWithError(err)
				w.mu.Lock()
				w.err = err
				w.mu.Unlock()
				return
			}
			prog := progress.WithPrefix(status, "", false)
			progress.FromReader(prog, "importing to docker", resp.Body)
		},
		done:   done,
		cancel: cancel,
	}
	return w, func() {
		pr.Close()
	}, nil
}

type waitingWriter struct {
	*io.PipeWriter
	f      func()
	once   sync.Once
	mu     sync.Mutex
	err    error
	done   chan struct{}
	cancel func()
}

func (w *waitingWriter) Write(dt []byte) (int, error) {
	w.once.Do(func() {
		go w.f()
	})
	return w.PipeWriter.Write(dt)
}

func (w *waitingWriter) Close() error {
	err := w.PipeWriter.Close()
	<-w.done
	if err == nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.err
	}
	return err
}

// ClientForEndpoint returns a docker client for an endpoint
func ClientForEndpoint(dockerCli command.Cli, name string) (client.APIClient, error) {
	dem, err := GetDockerEndpoint(dockerCli, name)
	if err == nil && dem != nil {
		ep, err := docker.WithTLSData(dockerCli.ContextStore(), name, *dem)
		if err != nil {
			return nil, err
		}
		clientOpts, err := ep.ClientOpts()
		if err != nil {
			return nil, err
		}
		return client.NewClientWithOpts(append(clientOpts, client.WithAPIVersionNegotiation())...)
	}
	ep := docker.Endpoint{
		EndpointMeta: docker.EndpointMeta{
			Host: name,
		},
	}
	clientOpts, err := ep.ClientOpts()
	if err != nil {
		return nil, err
	}
	return client.NewClientWithOpts(append(clientOpts, client.WithAPIVersionNegotiation())...)
}
