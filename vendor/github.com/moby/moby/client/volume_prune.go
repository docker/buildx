package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/volume"
)

// VolumePruneOptions holds parameters to prune volumes.
type VolumePruneOptions struct {
	// All controls whether named volumes should also be pruned. By
	// default, only anonymous volumes are pruned.
	All bool

	// Filters to apply when pruning.
	Filters Filters
}

// VolumePruneResult holds the result from the [Client.VolumesPrune] method.
type VolumePruneResult struct {
	Report volume.PruneReport
}

// VolumesPrune requests the daemon to delete unused data
func (cli *Client) VolumesPrune(ctx context.Context, opts VolumePruneOptions) (VolumePruneResult, error) {
	if opts.All {
		if _, ok := opts.Filters["all"]; ok {
			return VolumePruneResult{}, errdefs.ErrInvalidArgument.WithMessage(`conflicting options: cannot specify both "all" and "all" filter`)
		}
		if opts.Filters == nil {
			opts.Filters = Filters{}
		}
		opts.Filters.Add("all", "true")
	}

	query := url.Values{}
	opts.Filters.updateURLValues(query)

	resp, err := cli.post(ctx, "/volumes/prune", query, nil, nil)
	defer ensureReaderClosed(resp)
	if err != nil {
		return VolumePruneResult{}, err
	}

	var report volume.PruneReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return VolumePruneResult{}, fmt.Errorf("Error retrieving volume prune report: %v", err)
	}

	return VolumePruneResult{Report: report}, nil
}
