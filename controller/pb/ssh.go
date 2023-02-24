package pb

import (
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
)

func CreateSSH(ssh []*SSH) (session.Attachable, error) {
	configs := make([]sshprovider.AgentConfig, 0, len(ssh))
	for _, ssh := range ssh {
		cfg := sshprovider.AgentConfig{
			ID:    ssh.ID,
			Paths: append([]string{}, ssh.Paths...),
		}
		configs = append(configs, cfg)
	}
	return sshprovider.NewSSHAgentProvider(configs)
}
