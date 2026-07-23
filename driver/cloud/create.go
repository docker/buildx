package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/pkg/errors"
)

const hubHostDefault = "https://hub.docker.com"

// cloudBuilder represents a builder instance returned by the Hub Cloud Builds API.
type cloudBuilder struct {
	// Name of the builder e.g. linux-amd64.
	Name string `json:"name"`
	// Platform of the builder e.g. linux/amd64.
	Platform string `json:"platform"`
	// Endpoint of the builder e.g. cloud://myorg/mybuilder_linux-amd64
	Endpoint string `json:"endpoint"`
	// DataPlane contains data plane info for the builder
	DataPlane cloudBuilderDataPlane `json:"data_plane"`
}

type cloudBuilderDataPlane struct {
	// Name is the logical name of the data plane e.g. us-east-1
	Name string `json:"name"`
	// DisplayName is the name to display to users e.g. US East
	DisplayName string `json:"display_name"`
	// ProxyEndpoint is the address of the data plane's proxy API e.g. tcp://build-cloud.docker.com:443
	ProxyEndpoint string `json:"proxy_endpoint"`
	// RegistryEndpoint is the address of the data plane's registry API e.g. https://build-cloud.docker.com:443
	RegistryEndpoint string `json:"registry_endpoint"`
	// HealthEndpoint is the address of the data plane's health check API e.g. https://build-cloud.docker.com:443/v2
	HealthEndpoint string `json:"health_endpoint"`
}

// Arch returns the architecture of the builder.
// example: for platform linux/amd64, returns amd64.
func (b cloudBuilder) Arch() string {
	parts := strings.Split(b.Platform, "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// resolveBuilderInstances calls the Hub Cloud Builds API to get
// a list of builder instances under a builder group.
func resolveBuilderInstances(ctx context.Context, group string, driverOpts map[string]string) ([]cloudBuilder, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, 30*time.Second, errors.WithStack(context.DeadlineExceeded))
	defer cancel()
	registryHostname := hubRegistryEntry
	hubHost := hubHostDefault
	for k, v := range driverOpts {
		switch k {
		case "internal.auth.entry":
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			registryHostname = v
		case "internal.hub.host":
			hubHost = v
		}
	}

	token, err := fetchHubToken(ctx, registryHostname, hubHost)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch hub token")
	}

	builders, err := getBuilderInstances(ctx, group, hubHost, token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get builders")
	}

	if len(builders) == 0 {
		return nil, errors.Errorf("no builders found for group: %s", group)
	}

	// Bubble builders with arch matching local to the front.
	// Preserves order otherwise.
	sort.SliceStable(builders, func(i, j int) bool {
		return builders[i].Arch() == runtime.GOARCH && builders[j].Arch() != runtime.GOARCH
	})

	return builders, nil
}

// getBuilderInstanceDriverOpts creates the instance specific driver opts
// from the driver-opt args and the builder instance details
func getBuilderInstanceDriverOpts(builderInstance cloudBuilder, driverOpts map[string]string) map[string]string {
	opts := maps.Clone(driverOpts)
	// If builder instance has a data plane endpoint and it has not been overridden by the driver opts then apply it
	if opts[optKeyInternalAddress] == "" {
		if opts == nil {
			opts = make(map[string]string)
		}
		if opts[optKeyInternalProxyAddress] == "" && builderInstance.DataPlane.ProxyEndpoint != "" {
			opts[optKeyInternalProxyAddress] = builderInstance.DataPlane.ProxyEndpoint
		}
		if opts[optKeyInternalRegistryAddress] == "" && builderInstance.DataPlane.RegistryEndpoint != "" {
			opts[optKeyInternalRegistryAddress] = builderInstance.DataPlane.RegistryEndpoint
		}
		if opts[optKeyInternalHealthAddress] == "" && builderInstance.DataPlane.HealthEndpoint != "" {
			opts[optKeyInternalHealthAddress] = builderInstance.DataPlane.HealthEndpoint
		}
	}
	return opts
}

func (*factory) ResolveNodes(ctx context.Context, node driver.Node) ([]driver.Node, error) {
	if node.Endpoint == "" {
		return nil, errors.Errorf("no endpoint (builder) provided")
	}
	builders, err := resolveBuilderInstances(ctx, node.Endpoint, node.DriverOpts)
	if err != nil {
		return nil, errors.Wrap(err, "unknown builder")
	}

	nodes := make([]driver.Node, len(builders))
	for i, builder := range builders {
		nodes[i] = driver.Node{
			Name:        builder.Name,
			Endpoint:    builder.Endpoint,
			Platforms:   []string{builder.Platform},
			EndpointSet: false,
			DriverOpts:  getBuilderInstanceDriverOpts(builder, node.DriverOpts),
		}
	}
	return nodes, nil
}

// getBuilderInstances calls the Hub Cloud Builds API to get
// a list of builder instances under a builder group.
func getBuilderInstances(ctx context.Context, builderGroup, hubHost, token string) ([]cloudBuilder, error) {
	parts := strings.Split(builderGroup, "/")
	namespace := parts[0]
	group := parts[1]
	url := fmt.Sprintf("%s/v2/cloud-builds/accounts/%s/builder-groups/%s/instances", hubHost, namespace, group)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("response status code: " + resp.Status)
	}

	// ignoring pagination as the default page size of 10 should include all builders.
	var respPayload struct {
		Results []cloudBuilder `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
		return nil, errors.Wrap(err, "decode get instances response")
	}

	return respPayload.Results, nil
}
