package dap

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/buildx/dap/common"
	"github.com/docker/buildx/util/daptest"
	"github.com/google/go-dap"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

func TestLaunch(t *testing.T) {
	adapter, conn, client := NewTestAdapter[common.Config](t)

	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()

	eg, _ := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := adapter.Start(ctx, conn)
		assert.NoError(t, err)
		return nil
	})

	var (
		initialized       = make(chan struct{})
		configurationDone <-chan *dap.ConfigurationDoneResponse
	)

	client.RegisterEvent("initialized", func(em dap.EventMessage) {
		// Send configuration done since we don't do any configuration.
		configurationDone = daptest.DoRequest[*dap.ConfigurationDoneResponse](t, client, &dap.ConfigurationDoneRequest{
			Request: dap.Request{Command: "configurationDone"},
		})
		close(initialized)
	})

	eg.Go(func() error {
		initializeResp := <-daptest.DoRequest[*dap.InitializeResponse](t, client, &dap.InitializeRequest{
			Request: dap.Request{Command: "initialize"},
		})
		assert.True(t, initializeResp.Success)
		assert.True(t, initializeResp.Body.SupportsConfigurationDoneRequest)

		launchResp := <-daptest.DoRequest[*dap.LaunchResponse](t, client, &dap.LaunchRequest{
			Request: dap.Request{Command: "launch"},
		})
		assert.True(t, launchResp.Success)

		// We should have received the initialized event.
		select {
		case <-initialized:
		default:
			assert.Fail(t, "did not receive initialized event")
		}

		select {
		case <-configurationDone:
		case <-time.After(10 * time.Second):
			assert.Fail(t, "did not receive configurationDone response")
		}
		return nil
	})

	eg.Wait()
}

// TestSetBreakpoints will test sending a setBreakpoints request with no breakpoints.
// The response should be an empty array instead of null in the JSON.
func TestSetBreakpoints(t *testing.T) {
	adapter, conn, client := NewTestAdapter[common.Config](t)

	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()

	eg, _ := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := adapter.Start(ctx, conn)
		assert.NoError(t, err)
		return nil
	})

	var (
		initialized    = make(chan struct{})
		setBreakpoints <-chan *dap.SetBreakpointsResponse
	)

	client.RegisterEvent("initialized", func(em dap.EventMessage) {
		setBreakpoints = daptest.DoRequest[*dap.SetBreakpointsResponse](t, client, &dap.SetBreakpointsRequest{
			Request: dap.Request{Command: "setBreakpoints"},
			Arguments: dap.SetBreakpointsArguments{
				Source:      dap.Source{Name: "Dockerfile", Path: filepath.Join(t.TempDir(), "Dockerfile")},
				Breakpoints: []dap.SourceBreakpoint{},
			},
		})
		close(initialized)
	})

	eg.Go(func() error {
		initializeResp := <-daptest.DoRequest[*dap.InitializeResponse](t, client, &dap.InitializeRequest{
			Request: dap.Request{Command: "initialize"},
		})
		assert.True(t, initializeResp.Success)
		assert.True(t, initializeResp.Body.SupportsConfigurationDoneRequest)

		launchResp := <-daptest.DoRequest[*dap.LaunchResponse](t, client, &dap.LaunchRequest{
			Request: dap.Request{Command: "launch"},
		})
		assert.True(t, launchResp.Success)

		// We should have received the initialized event.
		select {
		case <-initialized:
		default:
			assert.Fail(t, "did not receive initialized event")
		}

		select {
		case setBreakpointsResp := <-setBreakpoints:
			assert.True(t, setBreakpointsResp.Success)
			assert.Len(t, setBreakpointsResp.Body.Breakpoints, 0)
			assert.NotNil(t, setBreakpointsResp.Body.Breakpoints, "breakpoints should be an empty array instead of null in the JSON")
		case <-time.After(10 * time.Second):
			assert.Fail(t, "did not receive setBreakpoints response")
		}
		return nil
	})

	eg.Wait()
}

func TestBreakpointMapIntersectVerified(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	filename := "Dockerfile"
	fpath := filepath.Join(ws, filename)

	type breakpointCase struct {
		desc           string
		sbp            dap.SourceBreakpoint
		expectVerified bool
	}

	docRanges := []*pb.Range{
		{Start: &pb.Position{Line: 10, Character: 0}, End: &pb.Position{Line: 10, Character: 10}},
		{Start: &pb.Position{Line: 20, Character: 5}, End: &pb.Position{Line: 20, Character: 5}},
		{Start: &pb.Position{Line: 30, Character: 0}, End: &pb.Position{Line: 30, Character: 10}},
		{Start: &pb.Position{Line: 35, Character: 2}, End: &pb.Position{Line: 35, Character: 7}},
	}

	breakpointCases := []breakpointCase{
		{desc: "inside range 0", sbp: dap.SourceBreakpoint{Line: 10, Column: 5}, expectVerified: true},
		{desc: "before range 0", sbp: dap.SourceBreakpoint{Line: 10, Column: -1}},
		{desc: "range 1 point", sbp: dap.SourceBreakpoint{Line: 20, Column: 5}, expectVerified: true},
		{desc: "before range 1 point", sbp: dap.SourceBreakpoint{Line: 20, Column: 4}},
		{desc: "range 2 end", sbp: dap.SourceBreakpoint{Line: 30, Column: 10}, expectVerified: true},
		{desc: "after range 2", sbp: dap.SourceBreakpoint{Line: 30, Column: 11}},
		{desc: "inside range 3", sbp: dap.SourceBreakpoint{Line: 35, Column: 4}, expectVerified: true},
		{desc: "different line", sbp: dap.SourceBreakpoint{Line: 40, Column: 0}},
	}

	bm := newBreakpointMap()
	sbps := make([]dap.SourceBreakpoint, len(breakpointCases))
	for i, bc := range breakpointCases {
		sbps[i] = bc.sbp
	}
	bm.Set(fpath, sbps)

	srcLocs := make(map[string]*pb.Locations, len(docRanges))
	for i, rng := range docRanges {
		srcLocs[fmt.Sprintf("doc-%d", i)] = &pb.Locations{
			Locations: []*pb.Location{{
				SourceIndex: 0,
				Ranges:      []*pb.Range{rng},
			}},
		}
	}

	src := &pb.Source{
		Locations: srcLocs,
		Infos: []*pb.SourceInfo{
			{Filename: filename},
		},
	}

	ctx := newBreakpointTestContext(t)
	digests := bm.Intersect(ctx, src, ws)
	wantMatches := 0
	for _, bc := range breakpointCases {
		if bc.expectVerified {
			wantMatches++
		}
	}
	assert.Len(t, digests, wantMatches)

	expectedEvents := make(map[int]struct{})
	for i, bp := range bm.byPath[fpath] {
		if breakpointCases[i].expectVerified {
			expectedEvents[bp.Id] = struct{}{}
		}
	}

	for len(expectedEvents) > 0 {
		select {
		case msg := <-ctx.messages:
			evt, ok := msg.(*dap.BreakpointEvent)
			if !assert.True(t, ok, "expected breakpoint event message") {
				continue
			}
			if _, ok := expectedEvents[evt.Body.Breakpoint.Id]; ok {
				delete(expectedEvents, evt.Body.Breakpoint.Id)
				assert.True(t, evt.Body.Breakpoint.Verified)
			} else {
				t.Fatalf("unexpected breakpoint event for id %d", evt.Body.Breakpoint.Id)
			}
		case <-time.After(time.Second):
			t.Fatalf("expected %d more breakpoint events", len(expectedEvents))
		}
	}

	stored := bm.byPath[fpath]
	if assert.Len(t, stored, len(breakpointCases)) {
		for i, bc := range breakpointCases {
			assert.Equal(t, bc.expectVerified, stored[i].Verified, "breakpoint %d (%s) mismatch", i, bc.desc)
		}
	}
}

func NewTestAdapter[C LaunchConfig](t *testing.T) (*Adapter[C], Conn, *daptest.Client) {
	t.Helper()

	rd1, wr1 := io.Pipe()
	rd2, wr2 := io.Pipe()

	srvConn := daptest.LogConn(t, "server", NewConn(rd1, wr2))
	t.Cleanup(func() {
		srvConn.Close()
	})

	clientConn := daptest.LogConn(t, "client", NewConn(rd2, wr1))
	t.Cleanup(func() { clientConn.Close() })

	adapter := New[C]()
	t.Cleanup(func() { adapter.Stop() })

	client := daptest.NewClient(clientConn)
	t.Cleanup(func() { client.Close() })

	return adapter, srvConn, client
}

type breakpointTestContext struct {
	context.Context
	messages chan dap.Message
}

func newBreakpointTestContext(t *testing.T) *breakpointTestContext {
	t.Helper()
	return &breakpointTestContext{
		Context:  context.Background(),
		messages: make(chan dap.Message, 16),
	}
}

func (c *breakpointTestContext) C() chan<- dap.Message {
	return c.messages
}

func (c *breakpointTestContext) Go(f func(Context)) bool {
	go f(c)
	return true
}

func (c *breakpointTestContext) Request(dap.RequestMessage) dap.ResponseMessage {
	return nil
}
