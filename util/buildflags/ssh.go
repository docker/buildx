package buildflags

import (
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/moby/buildkit/util/gitutil"
)

func ParseSSHSpecs(sl []string) ([]*controllerapi.SSH, error) {
	var outs []*controllerapi.SSH
	if len(sl) == 0 {
		return nil, nil
	}

	for _, s := range sl {
		parts := strings.SplitN(s, "=", 2)
		out := controllerapi.SSH{
			ID: parts[0],
		}
		if len(parts) > 1 {
			out.Paths = strings.Split(parts[1], ",")
		}
		outs = append(outs, &out)
	}
	return outs, nil
}

// IsGitSSH returns true if the given repo URL is accessed over ssh
func IsGitSSH(url string) bool {
	_, gitProtocol := gitutil.ParseProtocol(url)
	return gitProtocol == gitutil.SSHProtocol
}
