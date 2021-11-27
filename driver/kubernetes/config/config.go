package config

import (
	"net/url"
	"os"
	"strings"

	cxtkubernetes "github.com/docker/cli/cli/context/kubernetes"
	ctxstore "github.com/docker/cli/cli/context/store"
	"k8s.io/client-go/tools/clientcmd"
)

// FromContext loads k8s config from context
func FromContext(endpointName string, s ctxstore.Reader) (clientcmd.ClientConfig, error) {
	if strings.HasPrefix(endpointName, "kubernetes://") {
		u, _ := url.Parse(endpointName)
		if kubeconfig := u.Query().Get("kubeconfig"); kubeconfig != "" {
			_ = os.Setenv(clientcmd.RecommendedConfigPathEnvVar, kubeconfig)
		}
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		apiConfig, err := rules.Load()
		if err != nil {
			return nil, err
		}
		return clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}), nil
	}
	return cxtkubernetes.ConfigFromContext(endpointName, s)
}
