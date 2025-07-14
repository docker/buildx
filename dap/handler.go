package dap

import (
	"context"
	"reflect"

	"github.com/google/go-dap"
	"github.com/pkg/errors"
)

type Context interface {
	context.Context
	C() chan<- dap.Message
	Go(f func(c Context)) bool
}

type dispatchContext struct {
	context.Context
	srv *Server
	ch  chan<- dap.Message
}

func (c *dispatchContext) C() chan<- dap.Message {
	return c.ch
}

func (c *dispatchContext) Go(f func(c Context)) bool {
	return c.srv.Go(f)
}

type HandlerFunc[Req dap.RequestMessage, Resp dap.ResponseMessage] func(c Context, req Req, resp Resp) error

func (h HandlerFunc[Req, Resp]) Do(c Context, req Req) (resp Resp, err error) {
	if h == nil {
		return resp, errors.New("not implemented")
	}

	respT := reflect.TypeFor[Resp]()
	rv := reflect.New(respT.Elem())
	resp = rv.Interface().(Resp)
	err = h(c, req, resp)
	return resp, err
}

type Handler struct {
	Initialize        HandlerFunc[*dap.InitializeRequest, *dap.InitializeResponse]
	Launch            HandlerFunc[*dap.LaunchRequest, *dap.LaunchResponse]
	Attach            HandlerFunc[*dap.AttachRequest, *dap.AttachResponse]
	SetBreakpoints    HandlerFunc[*dap.SetBreakpointsRequest, *dap.SetBreakpointsResponse]
	ConfigurationDone HandlerFunc[*dap.ConfigurationDoneRequest, *dap.ConfigurationDoneResponse]
	Disconnect        HandlerFunc[*dap.DisconnectRequest, *dap.DisconnectResponse]
	Terminate         HandlerFunc[*dap.TerminateRequest, *dap.TerminateResponse]
	Continue          HandlerFunc[*dap.ContinueRequest, *dap.ContinueResponse]
	Next              HandlerFunc[*dap.NextRequest, *dap.NextResponse]
	StepIn            HandlerFunc[*dap.StepInRequest, *dap.StepInResponse]
	StepOut           HandlerFunc[*dap.StepOutRequest, *dap.StepOutResponse]
	Restart           HandlerFunc[*dap.RestartRequest, *dap.RestartResponse]
	Threads           HandlerFunc[*dap.ThreadsRequest, *dap.ThreadsResponse]
	StackTrace        HandlerFunc[*dap.StackTraceRequest, *dap.StackTraceResponse]
	Evaluate          HandlerFunc[*dap.EvaluateRequest, *dap.EvaluateResponse]
	Source            HandlerFunc[*dap.SourceRequest, *dap.SourceResponse]
}
