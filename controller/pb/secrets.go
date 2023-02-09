package pb

import (
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
)

func CreateSecrets(secrets []*Secret) (session.Attachable, error) {
	fs := make([]secretsprovider.Source, 0, len(secrets))
	for _, secret := range secrets {
		fs = append(fs, secretsprovider.Source{
			ID:       secret.ID,
			FilePath: secret.FilePath,
			Env:      secret.Env,
		})
	}
	store, err := secretsprovider.NewStore(fs)
	if err != nil {
		return nil, err
	}
	return secretsprovider.NewSecretProvider(store), nil
}
