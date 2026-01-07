package policy

import (
	"context"
	"testing"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	moby_buildkit_v1_sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	solverpb "github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"
)

func TestMultiPolicyCallbackNoCallbacks(t *testing.T) {
	cb := MultiPolicyCallback()

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW, decision.Action)
}

func TestMultiPolicyCallbackAllowOnly(t *testing.T) {
	cb := MultiPolicyCallback(func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
		return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
	})

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW, decision.Action)
}

func TestMultiPolicyCallbackDenyWins(t *testing.T) {
	denyMsgA := &policysession.DenyMessage{Message: "nope-a"}
	denyMsgB := &policysession.DenyMessage{Message: "nope-b"}

	cb := MultiPolicyCallback(
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
		},
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{
				Action:       moby_buildkit_v1_sourcepolicy.PolicyAction_DENY,
				DenyMessages: []*policysession.DenyMessage{denyMsgA, denyMsgB},
			}, nil, nil
		},
	)

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_DENY, decision.Action)
	require.Equal(t, []*policysession.DenyMessage{denyMsgA, denyMsgB}, decision.DenyMessages)
}

func TestMultiPolicyCallbackDenyShortCircuits(t *testing.T) {
	called := false
	cb := MultiPolicyCallback(
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_DENY}, nil, nil
		},
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			called = true
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
		},
	)

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_DENY, decision.Action)
	require.False(t, called)
}

func TestMultiPolicyCallbackUpdateReturned(t *testing.T) {
	update := &solverpb.SourceOp{}
	cb := MultiPolicyCallback(
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
		},
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{
				Action: moby_buildkit_v1_sourcepolicy.PolicyAction_CONVERT,
				Update: update,
			}, nil, nil
		},
	)

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_CONVERT, decision.Action)
	require.Equal(t, update, decision.Update)
}

func TestMultiPolicyCallbackMetaRequest(t *testing.T) {
	metaReq := &gwpb.ResolveSourceMetaRequest{}
	cb := MultiPolicyCallback(
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return nil, metaReq, nil
		},
		func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_DENY}, nil, nil
		},
	)

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, decision)
	require.Equal(t, metaReq, meta)
}

func TestMultiPolicyCallbackNilCallbackIgnored(t *testing.T) {
	cb := MultiPolicyCallback(nil, func(context.Context, *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
		return &policysession.DecisionResponse{Action: moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW}, nil, nil
	})

	decision, meta, err := cb(context.Background(), &policysession.CheckPolicyRequest{})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.NotNil(t, decision)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW, decision.Action)
}
