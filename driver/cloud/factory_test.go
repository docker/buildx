package cloud

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBuilderName(t *testing.T) {
	t.Parallel()

	factory := &factory{}
	namer, ok := any(factory).(driver.DefaultBuilderNamer)
	require.True(t, ok)
	name, err := namer.DefaultBuilderName(t.Context(), "Org/Builder")
	require.NoError(t, err)
	assert.Equal(t, "cloud-org-builder", name)
}

func TestTokenRefresh(t *testing.T) {
	t.Parallel()

	now := time.Now()
	count := 0
	s := func(context.Context) (string, error) {
		count++
		return fmt.Sprintf("token-%d", count), nil
	}
	nextToken := newTokenSource(s, func() time.Time { return now })

	token, err := nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)

	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)

	now = now.Add(10 * time.Minute)
	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-2", token)
}

func TestGetUserContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		env      []string
	}{
		{
			name:     "empty env",
			env:      []string{},
			expected: "local",
		},
		{
			name:     "CI env var must be set or fallback to local",
			env:      []string{"CIRCLECI=true"},
			expected: "local",
		},
		{
			name:     "unknown CI",
			env:      []string{"CI=true"},
			expected: "ci",
		},
		{
			name:     "github ci detection",
			env:      []string{"CI=true", "GITHUB_ACTIONS=true"},
			expected: "github",
		},
		{
			name:     "gitlab ci detection",
			env:      []string{"CI=true", "GITLAB_CI=true"},
			expected: "gitlab",
		},
		{
			name:     "circleci ci detection",
			env:      []string{"CI=true", "CIRCLECI=true"},
			expected: "circleci",
		},
		{
			name:     "travis ci detection",
			env:      []string{"CI=true", "TRAVIS=true"},
			expected: "travis",
		},
		{
			name:     "buildkite ci detection",
			env:      []string{"CI=true", "BUILDKITE=true"},
			expected: "buildkite",
		},
		{
			name:     "jenkins ci detection",
			env:      []string{"CI=true", "JENKINS_HOME=/root/jenkins"},
			expected: "jenkins",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userContext := getUserContext(tt.env)
			assert.Equal(t, tt.expected, userContext)
		})
	}
}
