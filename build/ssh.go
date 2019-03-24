package build

import (
	"strings"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
)

func ParseSSHSpecs(sl []string) (session.Attachable, error) {
	configs := make([]sshprovider.AgentConfig, 0, len(sl))
	for _, v := range sl {
		c, err := parseSSH(v)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *c)
	}
	return sshprovider.NewSSHAgentProvider(configs)
}

func parseSSH(value string) (*sshprovider.AgentConfig, error) {
	parts := strings.SplitN(value, "=", 2)
	cfg := sshprovider.AgentConfig{
		ID: parts[0],
	}
	if len(parts) > 1 {
		cfg.Paths = strings.Split(parts[1], ",")
	}
	return &cfg, nil
}
