package ghutil

import (
	"os"
	"path"
	"testing"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/stretchr/testify/require"
)

func TestGithubActionsContext(t *testing.T) {
	for _, tt := range []string{"pr", "push", "tag"} {
		t.Run(tt, func(t *testing.T) {
			envs, err := dotenv.Read(path.Join("fixtures", "ghactx_"+tt+".env"))
			require.NoError(t, err)
			for k, v := range envs {
				t.Setenv(k, v)
			}
			ghactx, err := GithubActionsContext()
			require.NoError(t, err)
			require.NotNil(t, ghactx)
			expected, err := os.ReadFile("fixtures/ghactx_" + tt + "_expected.json")
			require.NoError(t, err)
			require.JSONEq(t, string(expected), string(ghactx))
		})
	}
}
