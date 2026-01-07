package policy

import (
	"context"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	moby_buildkit_v1_sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
)

// MultiPolicyCallback returns a policy callback that requires all policies to allow.
func MultiPolicyCallback(callbacks ...policysession.PolicyCallback) policysession.PolicyCallback {
	return func(ctx context.Context, req *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
		if len(callbacks) == 0 {
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
		}

		for _, cb := range callbacks {
			if cb == nil {
				continue
			}
			decision, metaReq, err := cb(ctx, req)
			if err != nil {
				return nil, nil, err
			}
			if metaReq != nil {
				return nil, metaReq, nil
			}
			if decision == nil {
				continue
			}

			switch decision.Action {
			case moby_buildkit_v1_sourcepolicy.PolicyAction_DENY:
				return decision, nil, nil
			case moby_buildkit_v1_sourcepolicy.PolicyAction_CONVERT:
				return decision, nil, nil
			case moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW:
				// noop
			default:
				// treat unknown actions as allow
			}
		}

		return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
	}
}
