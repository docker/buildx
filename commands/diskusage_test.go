package commands

import (
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	dclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToDiskUsageOptions(t *testing.T) {
	t.Run("without keep duration", func(t *testing.T) {
		pi := &client.PruneInfo{
			Filter: []string{"type==regular"},
		}
		opts := toDiskUsageOptions(pi)
		info := &client.DiskUsageInfo{}
		for _, o := range opts {
			o.SetDiskUsageOption(info)
		}
		assert.Equal(t, []string{"type==regular"}, info.Filter)
		assert.Equal(t, time.Duration(0), info.AgeLimit)
	})

	t.Run("with keep duration", func(t *testing.T) {
		pi := &client.PruneInfo{
			KeepDuration: 168 * time.Hour,
			Filter:       []string{"type==source.local"},
		}
		opts := toDiskUsageOptions(pi)
		info := &client.DiskUsageInfo{}
		for _, o := range opts {
			o.SetDiskUsageOption(info)
		}
		assert.Equal(t, []string{"type==source.local"}, info.Filter)
		assert.Equal(t, 168*time.Hour, info.AgeLimit)
	})
}

func TestToBuildkitPruneInfoUntilFilter(t *testing.T) {
	t.Run("until filter duration parsed", func(t *testing.T) {
		f := dclient.Filters{}
		f["until"] = map[string]bool{"168h": true}
		pi, err := toBuildkitPruneInfo(f)
		require.NoError(t, err)
		assert.Equal(t, 168*time.Hour, pi.KeepDuration)
		assert.Empty(t, pi.Filter[0])
	})

	t.Run("unused-for filter duration parsed", func(t *testing.T) {
		f := dclient.Filters{}
		f["unused-for"] = map[string]bool{"24h": true}
		pi, err := toBuildkitPruneInfo(f)
		require.NoError(t, err)
		assert.Equal(t, 24*time.Hour, pi.KeepDuration)
		assert.Empty(t, pi.Filter[0])
	})

	t.Run("conflicting until and unused-for", func(t *testing.T) {
		f := dclient.Filters{}
		f["until"] = map[string]bool{"24h": true}
		f["unused-for"] = map[string]bool{"48h": true}
		_, err := toBuildkitPruneInfo(f)
		require.Error(t, err)
	})

	t.Run("combined until and type filter", func(t *testing.T) {
		f := dclient.Filters{}
		f["until"] = map[string]bool{"72h": true}
		f["type"] = map[string]bool{"regular": true}
		pi, err := toBuildkitPruneInfo(f)
		require.NoError(t, err)
		assert.Equal(t, 72*time.Hour, pi.KeepDuration)
		assert.Equal(t, []string{"type==regular"}, pi.Filter)
	})
}
