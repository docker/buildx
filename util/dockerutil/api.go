package dockerutil

import (
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/docker"
	"github.com/docker/docker/client"
)

// ClientAPI represents an active docker API object.
type ClientAPI struct {
	client.APIClient
}

func NewClientAPI(cli command.Cli, ep string) (*ClientAPI, error) {
	ca := &ClientAPI{}

	var dep docker.Endpoint
	dem, err := GetDockerEndpoint(cli, ep)
	if err != nil {
		return nil, err
	} else if dem != nil {
		dep, err = docker.WithTLSData(cli.ContextStore(), ep, *dem)
		if err != nil {
			return nil, err
		}
	} else {
		dep = docker.Endpoint{
			EndpointMeta: docker.EndpointMeta{
				Host: ep,
			},
		}
	}

	clientOpts, err := dep.ClientOpts()
	if err != nil {
		return nil, err
	}

	ca.APIClient, err = client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, err
	}

	return ca, nil
}
