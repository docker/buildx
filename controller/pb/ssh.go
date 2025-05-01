package pb

import (
	"slices"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
)

type SSH struct {
	ID    string
	Paths []string
}

func CreateSSH(ssh []*SSH) (session.Attachable, error) {
	configs := make([]sshprovider.AgentConfig, 0, len(ssh))
	for _, ssh := range ssh {
		cfg := sshprovider.AgentConfig{
			ID:    ssh.ID,
			Paths: slices.Clone(ssh.Paths),
		}
		configs = append(configs, cfg)
	}
	return sshprovider.NewSSHAgentProvider(configs)
}
