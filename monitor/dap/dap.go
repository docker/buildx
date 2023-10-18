package dap

// Ported from https://github.com/ktock/buildg/blob/v0.4.1/pkg/dap/dap.go
// Copyright The buildg Authors.
// Licensed under the Apache License, Version 2.0

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/controller"
	"github.com/docker/buildx/controller/control"
	controllererror "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/walker"
	"github.com/docker/cli/cli/command"
	"github.com/google/go-dap"
	"github.com/google/shlex"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"golang.org/x/sync/errgroup"
)

const AttachContainerCommand = "_INTERNAL_DAP_ATTACH_CONTAINER"

func NewServer(dockerCli command.Cli, r io.Reader, w io.Writer) (*Server, error) {
	conn := &stdioConn{r, w}
	ctx, cancel := context.WithCancel(context.TODO())
	eg := new(errgroup.Group)
	s := &Server{
		conn:        conn,
		ctx:         ctx,
		cancel:      cancel,
		eg:          eg,
		breakpoints: &sync.Map{},
		dockerCli:   dockerCli,
	}
	return s, nil
}

type debugContext struct {
	breakCtx         atomic.Value
	launchConfig     *LaunchConfig
	dockerfileName   string
	walkerController *walker.Controller
	controller       control.BuildxController
	ref              string
	cancel           func()
}

type Server struct {
	conn   net.Conn
	sendMu sync.Mutex

	ctx    context.Context
	cancel func()
	eg     *errgroup.Group

	breakpoints *sync.Map
	dockerCli   command.Cli

	debugCtx atomic.Value
}

func (s *Server) Serve() error {
	var eg errgroup.Group
	r := bufio.NewReader(s.conn)
	for {
		req, err := dap.ReadProtocolMessage(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		eg.Go(func() error { return s.handle(req) })
	}
	return eg.Wait()
}

func (s *Server) send(message dap.Message) {
	s.sendMu.Lock()
	dap.WriteProtocolMessage(s.conn, message)
	logrus.WithField("dst", s.conn.RemoteAddr()).Debugf("message sent %+v", message)
	s.sendMu.Unlock()
}

func (s *Server) breakContext() *walker.BreakContext {
	var bCtx *walker.BreakContext
	if dbgCtx := s.debugContext(); dbgCtx != nil {
		if bc := dbgCtx.breakCtx.Load(); bc != nil {
			bCtx = bc.(*walker.BreakContext)
		}
	}
	return bCtx
}

func (s *Server) debugContext() *debugContext {
	if dbgCtx := s.debugCtx.Load(); dbgCtx != nil {
		return dbgCtx.(*debugContext)
	}
	return nil
}

const (
	unsupportedError = 1000
	failedError      = 1001
	unknownError     = 9999
)

var errorMessage = map[int]string{
	unsupportedError: "unsupported",
	failedError:      "failed",
	unknownError:     "unknown",
}

func (s *Server) sendErrorResponse(requestSeq int, command string, errID int, message string, showUser bool) {
	id, summary := unknownError, errorMessage[unknownError]
	if m, ok := errorMessage[errID]; ok {
		id, summary = errID, m
	}
	r := &dap.ErrorResponse{}
	r.Response = *newResponse(requestSeq, command)
	r.Success = false
	r.Message = summary
	r.Body.Error = &dap.ErrorMessage{}
	r.Body.Error.Format = message
	r.Body.Error.Id = id
	r.Body.Error.ShowUser = showUser
	s.send(r)
}

func (s *Server) sendUnsupportedResponse(requestSeq int, command string, message string) {
	s.sendErrorResponse(requestSeq, command, unsupportedError, message, false)
}

func (s *Server) outputStdoutWriter() io.Writer {
	return &outputWriter{s, "stdout"}
}

func (s *Server) handle(request dap.Message) error {
	logrus.Debugf("got request: %+v", request)
	switch request := request.(type) {
	case *dap.InitializeRequest:
		s.onInitializeRequest(request)
	case *dap.LaunchRequest:
		s.onLaunchRequest(request)
	case *dap.AttachRequest:
		s.onAttachRequest(request)
	case *dap.DisconnectRequest:
		s.onDisconnectRequest(request)
	case *dap.TerminateRequest:
		s.onTerminateRequest(request)
	case *dap.RestartRequest:
		s.onRestartRequest(request)
	case *dap.SetBreakpointsRequest:
		s.onSetBreakpointsRequest(request)
	case *dap.SetFunctionBreakpointsRequest:
		s.onSetFunctionBreakpointsRequest(request)
	case *dap.SetExceptionBreakpointsRequest:
		s.onSetExceptionBreakpointsRequest(request)
	case *dap.ConfigurationDoneRequest:
		s.onConfigurationDoneRequest(request)
	case *dap.ContinueRequest:
		s.onContinueRequest(request)
	case *dap.NextRequest:
		s.onNextRequest(request)
	case *dap.StepInRequest:
		s.onStepInRequest(request)
	case *dap.StepOutRequest:
		s.onStepOutRequest(request)
	case *dap.StepBackRequest:
		s.onStepBackRequest(request)
	case *dap.ReverseContinueRequest:
		s.onReverseContinueRequest(request)
	case *dap.RestartFrameRequest:
		s.onRestartFrameRequest(request)
	case *dap.GotoRequest:
		s.onGotoRequest(request)
	case *dap.PauseRequest:
		s.onPauseRequest(request)
	case *dap.StackTraceRequest:
		s.onStackTraceRequest(request)
	case *dap.ScopesRequest:
		s.onScopesRequest(request)
	case *dap.VariablesRequest:
		s.onVariablesRequest(request)
	case *dap.SetVariableRequest:
		s.onSetVariableRequest(request)
	case *dap.SetExpressionRequest:
		s.onSetExpressionRequest(request)
	case *dap.SourceRequest:
		s.onSourceRequest(request)
	case *dap.ThreadsRequest:
		s.onThreadsRequest(request)
	case *dap.TerminateThreadsRequest:
		s.onTerminateThreadsRequest(request)
	case *dap.EvaluateRequest:
		s.onEvaluateRequest(request)
	case *dap.StepInTargetsRequest:
		s.onStepInTargetsRequest(request)
	case *dap.GotoTargetsRequest:
		s.onGotoTargetsRequest(request)
	case *dap.CompletionsRequest:
		s.onCompletionsRequest(request)
	case *dap.ExceptionInfoRequest:
		s.onExceptionInfoRequest(request)
	case *dap.LoadedSourcesRequest:
		s.onLoadedSourcesRequest(request)
	case *dap.DataBreakpointInfoRequest:
		s.onDataBreakpointInfoRequest(request)
	case *dap.SetDataBreakpointsRequest:
		s.onSetDataBreakpointsRequest(request)
	case *dap.ReadMemoryRequest:
		s.onReadMemoryRequest(request)
	case *dap.DisassembleRequest:
		s.onDisassembleRequest(request)
	case *dap.CancelRequest:
		s.onCancelRequest(request)
	case *dap.BreakpointLocationsRequest:
		s.onBreakpointLocationsRequest(request)
	default:
		logrus.Warnf("Unable to process %#v\n", request)
	}
	return nil
}

func (s *Server) onInitializeRequest(request *dap.InitializeRequest) {
	response := &dap.InitializeResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body.SupportsConfigurationDoneRequest = true
	response.Body.SupportsFunctionBreakpoints = false
	response.Body.SupportsConditionalBreakpoints = false
	response.Body.SupportsHitConditionalBreakpoints = false
	response.Body.SupportsEvaluateForHovers = false
	response.Body.ExceptionBreakpointFilters = make([]dap.ExceptionBreakpointsFilter, 0)
	response.Body.SupportsStepBack = false
	response.Body.SupportsSetVariable = false
	response.Body.SupportsRestartFrame = false
	response.Body.SupportsGotoTargetsRequest = false
	response.Body.SupportsStepInTargetsRequest = false
	response.Body.SupportsCompletionsRequest = false
	response.Body.CompletionTriggerCharacters = make([]string, 0)
	response.Body.SupportsModulesRequest = false
	response.Body.AdditionalModuleColumns = make([]dap.ColumnDescriptor, 0)
	response.Body.SupportedChecksumAlgorithms = make([]dap.ChecksumAlgorithm, 0)
	response.Body.SupportsRestartRequest = false
	response.Body.SupportsExceptionOptions = false
	response.Body.SupportsValueFormattingOptions = false
	response.Body.SupportsExceptionInfoRequest = false
	response.Body.SupportTerminateDebuggee = false
	response.Body.SupportSuspendDebuggee = false
	response.Body.SupportsDelayedStackTraceLoading = false
	response.Body.SupportsLoadedSourcesRequest = false
	response.Body.SupportsLogPoints = false
	response.Body.SupportsTerminateThreadsRequest = false
	response.Body.SupportsSetExpression = false
	response.Body.SupportsTerminateRequest = false
	response.Body.SupportsDataBreakpoints = false
	response.Body.SupportsReadMemoryRequest = false
	response.Body.SupportsWriteMemoryRequest = false
	response.Body.SupportsDisassembleRequest = false
	response.Body.SupportsCancelRequest = false
	response.Body.SupportsBreakpointLocationsRequest = false
	response.Body.SupportsClipboardContext = false
	response.Body.SupportsSteppingGranularity = false
	response.Body.SupportsInstructionBreakpoints = false
	response.Body.SupportsExceptionFilterOptions = false

	s.send(response)
	s.send(&dap.InitializedEvent{Event: *newEvent("initialized")})
}

func (s *Server) onLaunchRequest(request *dap.LaunchRequest) {
	cfg := new(LaunchConfig)
	if err := json.Unmarshal(request.Arguments, cfg); err != nil {
		s.sendErrorResponse(request.Seq, request.Command, failedError, fmt.Sprintf("failed to launch: %v", err), true)
		return
	}
	if err := s.launchDebugger(*cfg); err != nil {
		s.sendErrorResponse(request.Seq, request.Command, failedError, fmt.Sprintf("failed to launch: %v", err), true)
		return
	}
	response := &dap.LaunchResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	s.send(response)
}

type LaunchConfig struct {
	Program     string `json:"program"`
	StopOnEntry bool   `json:"stopOnEntry"`

	Target    string   `json:"target"`
	BuildArgs []string `json:"build-args"`
	SSH       []string `json:"ssh"`
	Secrets   []string `json:"secrets"`

	Root           string `json:"root"`
	ControllerMode string `json:"controller-mode"` // "local" or "remote" (default)
	ServerConfig   string `json:"server-config"`
}

func parseLaunchConfig(cfg LaunchConfig) (bo controllerapi.BuildOptions, _ error) {
	if cfg.Program == "" {
		return bo, errors.Errorf("program must be specified")
	}

	contextPath, dockerfile := filepath.Split(cfg.Program)
	bo.ContextPath = contextPath
	bo.DockerfileName = filepath.Join(contextPath, dockerfile)
	if target := cfg.Target; target != "" {
		bo.Target = target
	}
	bo.BuildArgs = listToMap(cfg.BuildArgs, true)
	bo.Exports = append(bo.Exports, &controllerapi.ExportEntry{
		Type: "image",
	})
	var err error
	bo.Secrets, err = buildflags.ParseSecretSpecs(cfg.Secrets)
	if err != nil {
		return bo, err
	}
	bo.SSH, err = buildflags.ParseSSHSpecs(cfg.SSH)
	if err != nil {
		return bo, err
	}

	// TODO
	// - CacheFrom, CacheTo
	// - Contexts
	// - ExtraHosts
	// ...
	return bo, nil
}

func (s *Server) launchDebugger(cfg LaunchConfig) (retErr error) {
	if cfg.Program == "" {
		return errors.Errorf("launch error: program must be specified")
	}

	if dbgCtx := s.debugContext(); dbgCtx != nil && dbgCtx.cancel != nil {
		dbgCtx.cancel()
	}

	buildOpt, err := parseLaunchConfig(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer func() {
		if retErr != nil {
			cancel()
		}
	}()
	var printer *progress.Printer
	printer, err = progress.NewPrinter(ctx, s.outputStdoutWriter(), progressui.PlainMode,
		progress.WithOnClose(func() {
			printWarnings(s.outputStdoutWriter(), printer.Warnings())
		}),
	)
	if err != nil {
		return err
	}

	c, err := controller.NewController(ctx, control.ControlOptions{
		Detach:       cfg.ControllerMode != "local",
		ServerConfig: cfg.ServerConfig,
		Root:         cfg.Root,
	}, s.dockerCli, printer)
	if err != nil {
		return err
	}

	buildOpt.Debug = true // we don't get the result but get only the build definition via error.
	ref, _, err := c.Build(ctx, buildOpt, nil, printer)
	if err != nil {
		var be *controllererror.BuildError
		if errors.As(err, &be) {
			ref = be.Ref
			// We can proceed to dap session
		} else {
			return err
		}
	}

	st, err := c.Inspect(ctx, ref)
	if err != nil {
		return err
	}
	bpsA, _ := s.breakpoints.LoadOrStore(buildOpt.DockerfileName, walker.NewBreakpoints())
	bps := bpsA.(*walker.Breakpoints)
	if cfg.StopOnEntry {
		bps.Add("stopOnEntry", walker.NewStopOnEntryBreakpoint())
	} // TODO: clear on false?
	bps.Add("onError", walker.NewOnErrorBreakpoint()) // always break on error
	dbgCtx := &debugContext{}
	doneCh := make(chan struct{})
	wc := walker.NewController(st.Definition, bps,
		func(ctx context.Context, bCtx *walker.BreakContext) error {
			for key, r := range bCtx.Hits {
				logrus.Debugf("Breakpoint[%s]: %v", key, r)
			}
			breakpoints := make([]int, 0)
			for si := range bCtx.Hits {
				keyI, err := strconv.ParseInt(si, 10, 64)
				if err != nil {
					logrus.WithError(err).Warnf("failed to parse breakpoint key")
					continue
				}
				breakpoints = append(breakpoints, int(keyI))
			}
			reason := "breakpoint"
			if len(breakpoints) == 0 {
				reason = "step"
			}
			s.send(&dap.StoppedEvent{
				Event: *newEvent("stopped"),
				Body:  dap.StoppedEventBody{Reason: reason, ThreadId: 1, AllThreadsStopped: true, HitBreakpointIds: breakpoints},
			})
			dbgCtx.breakCtx.Store(bCtx)
			return nil
		},
		func(ctx context.Context, st llb.State) error {
			def, err := st.Marshal(ctx)
			if err != nil {
				return errors.Errorf("solve: failed to marshal definition: %v", err)
			}
			return c.Solve(ctx, ref, def.ToPB(), printer)
		},
		func(err error) {
			s.breakpoints.Delete(buildOpt.DockerfileName)
			s.send(&dap.ThreadEvent{Event: *newEvent("thread"), Body: dap.ThreadEventBody{Reason: "exited", ThreadId: 1}})
			s.send(&dap.TerminatedEvent{Event: *newEvent("terminated")})
			s.send(&dap.ExitedEvent{Event: *newEvent("exited")})
			close(doneCh)
		},
	)
	dbgCtx.launchConfig = &cfg
	dbgCtx.dockerfileName = buildOpt.DockerfileName
	dbgCtx.walkerController = wc
	dbgCtx.controller = c
	dbgCtx.ref = ref
	var once sync.Once
	dbgCtx.cancel = func() {
		once.Do(func() {
			cancel()
			wc.WalkCancel()
			<-doneCh
		})
	}
	s.debugCtx.Store(dbgCtx)

	// notify started
	s.send(&dap.ThreadEvent{Event: *newEvent("thread"), Body: dap.ThreadEventBody{Reason: "started", ThreadId: 1}})
	if err := wc.StartWalk(); err != nil {
		return err
	}

	return nil
}

func (s *Server) onDisconnectRequest(request *dap.DisconnectRequest) {
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.eg.Wait(); err != nil { // wait for container cleanup
		logrus.WithError(err).Warnf("failed to close tasks")
	}
	if dbgCtx := s.debugContext(); dbgCtx != nil && dbgCtx.cancel != nil {
		dbgCtx.cancel()
	}
	response := &dap.DisconnectResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	s.send(response)
	os.Exit(0) // TODO: return the control to the main func if needed
}

func (s *Server) onSetBreakpointsRequest(request *dap.SetBreakpointsRequest) {
	args := request.Arguments
	breakpoints := make([]dap.Breakpoint, 0)

	bpsA, _ := s.breakpoints.LoadOrStore(args.Source.Path, walker.NewBreakpoints())
	bps := bpsA.(*walker.Breakpoints)
	bps.ClearAll()
	bps.Add("onError", walker.NewOnErrorBreakpoint()) // always break on error
	for i := 0; i < len(args.Breakpoints); i++ {
		bp := walker.NewLineBreakpoint(int64(args.Breakpoints[i].Line))
		key, err := bps.Add("", bp)
		if err != nil {
			logrus.WithError(err).Warnf("failed to add breakpoints")
			continue
		}
		keyI, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			logrus.WithError(err).Warnf("failed to parse breakpoint key")
			continue
		}
		breakpoints = append(breakpoints, dap.Breakpoint{
			Id:       int(keyI),
			Source:   &args.Source,
			Line:     args.Breakpoints[i].Line,
			Verified: true,
		})
	}

	response := &dap.SetBreakpointsResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body.Breakpoints = breakpoints
	s.send(response)
}

func (s *Server) onConfigurationDoneRequest(request *dap.ConfigurationDoneRequest) {
	response := &dap.ConfigurationDoneResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	s.send(response)
}

func (s *Server) onContinueRequest(request *dap.ContinueRequest) {
	response := &dap.ContinueResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	s.send(response)
	if dbgCtx := s.debugContext(); dbgCtx != nil {
		if wc := dbgCtx.walkerController; wc != nil {
			wc.Continue()
		}
	}
}

func (s *Server) onNextRequest(request *dap.NextRequest) {
	response := &dap.NextResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	s.send(response)
	if dbgCtx := s.debugContext(); dbgCtx != nil {
		if wc := dbgCtx.walkerController; wc != nil {
			wc.Next()
		}
	}
}

func (s *Server) onStackTraceRequest(request *dap.StackTraceRequest) {
	response := &dap.StackTraceResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body = dap.StackTraceResponseBody{
		StackFrames: make([]dap.StackFrame, 0),
	}

	bCtx := s.breakContext()
	var launchConfig *LaunchConfig
	if dbgCtx := s.debugContext(); dbgCtx != nil {
		launchConfig = dbgCtx.launchConfig
	}
	if bCtx == nil || launchConfig == nil {
		// no stack trace is available now
		s.send(response)
		return
	}

	var lines []*pb.Range

	// If there are hit breakpoints on the current Op, return them.
	// FIXME: This is a workaround against stackFrame doesn't support
	//        multiple sources per frame. Once dap support it, we can
	//        return all current locations.
	// TODO: show non-breakpoint locations to output as well
	for _, ranges := range bCtx.Hits {
		lines = append(lines, ranges...)
	}
	if len(lines) == 0 {
		// no breakpoints on the current Op. This can happen on
		// step execution.
		for _, r := range bCtx.Cursors {
			rr := r
			lines = append(lines, &rr)
		}
	}
	if len(lines) > 0 {
		name := "instruction"
		if _, _, op, _, err := bCtx.State.Output().Vertex(context.TODO(), nil).Marshal(context.TODO(), nil); err == nil {
			if n, ok := op.Description["com.docker.dockerfile.v1.command"]; ok {
				name = n
			}
		}
		f := launchConfig.Program
		response.Body.StackFrames = []dap.StackFrame{
			{
				Id:     0,
				Source: &dap.Source{Name: filepath.Base(f), Path: f},
				// FIXME: We only return lines[0] because stackFrame doesn't support
				//        multiple sources per frame. Once dap support it, we can
				//        return all current locations.
				Line:    int(lines[0].Start.Line),
				EndLine: int(lines[0].End.Line),
				Name:    name,
			},
		}
		response.Body.TotalFrames = 1
	}
	s.send(response)
}

func (s *Server) onScopesRequest(request *dap.ScopesRequest) {
	response := &dap.ScopesResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body = dap.ScopesResponseBody{
		Scopes: []dap.Scope{
			{
				Name:               "Environment Variables",
				VariablesReference: 1,
			},
		},
	}
	s.send(response)
}

func (s *Server) onVariablesRequest(request *dap.VariablesRequest) {
	response := &dap.VariablesResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body = dap.VariablesResponseBody{
		Variables: make([]dap.Variable, 0), // neovim doesn't allow nil
	}

	bCtx := s.breakContext()
	if bCtx == nil {
		s.send(response)
		return
	}

	var variables []dap.Variable
	_, dt, _, _, err := bCtx.State.Output().Vertex(context.TODO(), nil).Marshal(context.TODO(), nil)
	if err != nil {
		logrus.WithError(err).Warnf("failed to marshal execop")
		s.send(response)
		return
	}
	var pbop pb.Op
	if err := pbop.Unmarshal(dt); err != nil {
		logrus.WithError(err).Warnf("failed to unmarshal execop")
		s.send(response)
		return
	}
	switch op := pbop.GetOp().(type) {
	case *pb.Op_Exec:
		for _, e := range op.Exec.Meta.Env {
			var k, v string
			if kv := strings.SplitN(e, "=", 2); len(kv) >= 2 {
				k, v = kv[0], kv[1]
			} else if len(kv) == 1 {
				k = kv[0]
			} else {
				continue
			}
			variables = append(variables, dap.Variable{
				Name:  k,
				Value: v,
			})
		}
	default:
		// TODO: support other Ops
	}

	if s := request.Arguments.Start; s > 0 {
		if s < len(variables) {
			variables = variables[s:]
		} else {
			variables = nil
		}
	}
	if c := request.Arguments.Count; c > 0 {
		if c < len(variables) {
			variables = variables[:c]
		}
	}
	response.Body.Variables = append(response.Body.Variables, variables...)
	s.send(response)
}

func (s *Server) onThreadsRequest(request *dap.ThreadsRequest) {
	response := &dap.ThreadsResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body = dap.ThreadsResponseBody{Threads: []dap.Thread{{Id: 1, Name: "build"}}}
	s.send(response)
}

type handlerContext struct {
	breakContext         *walker.BreakContext
	stdout               io.Writer
	evaluateDoneCallback func()
	controller           control.BuildxController
	ref                  string
	launchConfig         *LaunchConfig
}

type replCommand func(ctx context.Context, hCtx *handlerContext) cli.Command

func (s *Server) onEvaluateRequest(request *dap.EvaluateRequest) {
	if request.Arguments.Context != "repl" {
		s.sendUnsupportedResponse(request.Seq, request.Command, "evaluating non-repl input is unsupported as of now")
		return
	}

	bCtx := s.breakContext()
	if bCtx == nil {
		s.sendErrorResponse(request.Seq, request.Command, failedError, "no breakpoint available", true)
		return
	}

	replCommands := []replCommand{s.execCommand, s.psCommand, s.attachCommand}

	hCtx := new(handlerContext)
	out := new(bytes.Buffer)
	if args, err := shlex.Split(request.Arguments.Expression); err != nil {
		logrus.WithError(err).Warnf("failed to parse line")
	} else if len(args) > 0 && args[0] != "" {
		app := cli.NewApp()
		rootCmd := "buildx"
		app.Name = rootCmd
		app.HelpName = rootCmd
		app.Usage = "Buildx Interactive Debugger"
		app.UsageText = "command [command options] [arguments...]"
		app.ExitErrHandler = func(context *cli.Context, err error) {}
		app.UseShortOptionHandling = true
		app.Writer = out
		hCtx = &handlerContext{
			breakContext: bCtx,
			stdout:       out,
		}
		dbgCtx := s.debugContext()
		if dbgCtx == nil {
			s.sendErrorResponse(request.Seq, request.Command, failedError, "debugger isn't launched", true)
			return
		}
		hCtx.controller = dbgCtx.controller
		hCtx.ref = dbgCtx.ref
		hCtx.launchConfig = dbgCtx.launchConfig
		for _, fn := range replCommands {
			app.Commands = append(app.Commands, fn(s.ctx, hCtx)) // s.ctx is cancelled on disconnect
		}
		if err := app.Run(append([]string{rootCmd}, args...)); err != nil {
			out.WriteString(err.Error() + "\n")
		}
	}
	response := &dap.EvaluateResponse{}
	response.Response = *newResponse(request.Seq, request.Command)
	response.Body = dap.EvaluateResponseBody{
		Result: out.String(),
	}
	s.send(response)
	if hCtx.evaluateDoneCallback != nil {
		hCtx.evaluateDoneCallback()
	}
}

func (s *Server) psCommand(ctx context.Context, hCtx *handlerContext) cli.Command {
	return cli.Command{
		Name:      "ps",
		Usage:     "List attachable processes.",
		UsageText: "ps",
		Action: func(clicontext *cli.Context) (retErr error) {
			// gctx := s.ctx // cancelled on disconnect
			plist, err := hCtx.controller.ListProcesses(ctx, hCtx.ref)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(hCtx.stdout, 1, 8, 1, '\t', 0)
			fmt.Fprintln(tw, "PID\tCOMMAND")
			for _, p := range plist {
				fmt.Fprintf(tw, "%-20s\t%v\n", p.ProcessID, append(p.InvokeConfig.Entrypoint, p.InvokeConfig.Cmd...))
			}
			tw.Flush()
			return nil
		},
	}
}

func (s *Server) attachCommand(ctx context.Context, hCtx *handlerContext) cli.Command {
	return cli.Command{
		Name:      "attach",
		Usage:     "Attach to a processes.",
		UsageText: "attach PID",
		Action: func(clicontext *cli.Context) (retErr error) {
			args := clicontext.Args()
			if len(args) == 0 || args[0] == "" {
				return errors.Errorf("specify PID")
			}
			return s.invoke(ctx, hCtx, args[0], controllerapi.InvokeConfig{}, true, true)
		},
	}
}

func (s *Server) execCommand(ctx context.Context, hCtx *handlerContext) cli.Command {
	return cli.Command{
		Name:    "exec",
		Aliases: []string{"e"},
		Usage:   "Execute command in the step",
		UsageText: `exec [OPTIONS] [ARGS...]

If ARGS isn't provided, "/bin/sh" is used by default.
`,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "init-state",
				Usage: "Execute commands in an initial state of that step",
			},
			cli.BoolTFlag{
				Name:  "tty,t",
				Usage: "Allocate tty (enabled by default)",
			},
			cli.BoolTFlag{
				Name:  "i",
				Usage: "Enable stdin (FIXME: must be set with tty) (enabled by default)",
			},
			cli.StringSliceFlag{
				Name:  "env,e",
				Usage: "Set environment variables",
			},
			cli.StringFlag{
				Name:  "workdir,w",
				Usage: "Working directory inside the container",
			},
			cli.BoolFlag{
				Name:  "rollback",
				Usage: "Kill running processes and recreate the debugging container",
			},
		},
		Action: func(clicontext *cli.Context) (retErr error) {
			args := clicontext.Args()
			if len(args) == 0 || args[0] == "" {
				args = []string{"/bin/sh"}
			}
			flagI := clicontext.Bool("i")
			flagT := clicontext.Bool("tty")
			if flagI && !flagT || !flagI && flagT {
				return errors.Errorf("flag \"-i\" and \"-t\" must be set together") // FIXME
			}

			cwd, noCwd := "", false
			if c := clicontext.String("workdir"); c == "" {
				noCwd = true
			} else {
				cwd = c
			}
			rollback := clicontext.Bool("rollback")
			invokeConfig := controllerapi.InvokeConfig{
				Entrypoint: []string{args[0]},
				Cmd:        args[1:],
				Env:        clicontext.StringSlice("env"),
				NoUser:     true,
				Cwd:        cwd,
				NoCwd:      noCwd,
				Tty:        clicontext.Bool("tty"),
				Initial:    clicontext.Bool("init-state"),
				Rollback:   rollback,
			}
			pid := identity.NewID()

			return s.invoke(ctx, hCtx, pid, invokeConfig, flagI, flagT)
		},
	}
}

func (s *Server) invoke(ctx context.Context, hCtx *handlerContext, pid string, invokeConfig controllerapi.InvokeConfig, enableStdin, enableTty bool) (retErr error) {
	// gCtx := s.ctx // cancelled on disconnect
	var cleanups []func()
	defer func() {
		if retErr != nil {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		}
	}()

	// Prepare state dir
	tmpRoot, err := os.MkdirTemp("", "buildx-serve-state")
	if err != nil {
		return err
	}
	cleanups = append(cleanups, func() { os.RemoveAll(tmpRoot) })

	// Server IO
	logrus.Debugf("container root %+v", tmpRoot)
	stdin, stdout, stderr, done, err := serveContainerIO(ctx, tmpRoot)
	if err != nil {
		return err
	}
	cleanups = append(cleanups, func() { done() })

	// Search container client
	self, err := os.Executable()
	if err != nil {
		return err
	}

	if !enableStdin {
		stdin = nil
	}

	doneCh := make(chan struct{})
	errCh := make(chan error)
	go func() {
		defer close(doneCh)
		if err := hCtx.controller.Invoke(context.TODO(), hCtx.ref, pid, invokeConfig, stdin, stdout, stderr); err != nil {
			errCh <- err
			return
		}
	}()

	// Let the caller to attach to the container after evaluation response received.
	hCtx.evaluateDoneCallback = func() {
		s.send(&dap.RunInTerminalRequest{
			Request: dap.Request{
				ProtocolMessage: dap.ProtocolMessage{
					Seq:  0,
					Type: "request",
				},
				Command: "runInTerminal",
			},
			Arguments: dap.RunInTerminalRequestArguments{
				Kind:  "integrated",
				Title: "containerclient",
				Args:  []string{self, AttachContainerCommand, "--set-tty-raw=" + strconv.FormatBool(enableTty), tmpRoot},

				// TODO: use envvar once all editors support it.
				// Env:   map[string]interface{}{"BUILDX_EXPERIMENTAL": "1"},

				// emacs requires this nonempty otherwise error (Wrong type argument: stringp, nil) will occur on dap-ui-breakpoints()
				Cwd: filepath.Dir(hCtx.launchConfig.Program),
			},
		})
	}
	s.eg.Go(func() error {
		select {
		case <-doneCh:
			s.outputStdoutWriter().Write([]byte("container finished"))
		case err := <-errCh:
			s.outputStdoutWriter().Write([]byte(fmt.Sprintf("container finished(%v)", err)))
		case err := <-ctx.Done():
			s.outputStdoutWriter().Write([]byte(fmt.Sprintf("finishing container due to server shutdown: %v", err)))
		}
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		select {
		case <-doneCh:
		case err := <-errCh:
			s.outputStdoutWriter().Write([]byte(fmt.Sprintf("container exit(%v)", err)))
		case <-time.After(3 * time.Second):
			s.outputStdoutWriter().Write([]byte("container exit timeout"))
		}
		return nil
	})
	return nil
}

func (s *Server) onSetExceptionBreakpointsRequest(request *dap.SetExceptionBreakpointsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "Request unsupported")
}

func (s *Server) onRestartRequest(request *dap.RestartRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "RestartRequest unsupported")
}

func (s *Server) onAttachRequest(request *dap.AttachRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "AttachRequest unsupported")
}

func (s *Server) onTerminateRequest(request *dap.TerminateRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "TerminateRequest unsupported")
}

func (s *Server) onSetFunctionBreakpointsRequest(request *dap.SetFunctionBreakpointsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "FunctionBreakpointsRequest unsupported")
}

func (s *Server) onStepInRequest(request *dap.StepInRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "StepInRequest unsupported")
}

func (s *Server) onStepOutRequest(request *dap.StepOutRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "StepOutRequest unsupported")
}

func (s *Server) onStepBackRequest(request *dap.StepBackRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "StepBackRequest unsupported")
}

func (s *Server) onReverseContinueRequest(request *dap.ReverseContinueRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "ReverseContinueRequest unsupported")
}

func (s *Server) onRestartFrameRequest(request *dap.RestartFrameRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "RestartFrameRequest unsupported")
}

func (s *Server) onGotoRequest(request *dap.GotoRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "GotoRequest unsupported")
}

func (s *Server) onPauseRequest(request *dap.PauseRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "PauseRequest unsupported")
}

func (s *Server) onSetVariableRequest(request *dap.SetVariableRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "SetVariablesRequest unsupported")
}

func (s *Server) onSetExpressionRequest(request *dap.SetExpressionRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "SetExpressionRequest unsupported")
}

func (s *Server) onSourceRequest(request *dap.SourceRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "SourceRequest unsupported")
}

func (s *Server) onTerminateThreadsRequest(request *dap.TerminateThreadsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "TerminateRequest unsupported")
}

func (s *Server) onStepInTargetsRequest(request *dap.StepInTargetsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "StepInTargetsRequest unsupported")
}

func (s *Server) onGotoTargetsRequest(request *dap.GotoTargetsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "GotoTargetsRequest unsupported")
}

func (s *Server) onCompletionsRequest(request *dap.CompletionsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "CompletionsRequest unsupported")
}

func (s *Server) onExceptionInfoRequest(request *dap.ExceptionInfoRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "ExceptionInfoRequest unsupported")
}

func (s *Server) onLoadedSourcesRequest(request *dap.LoadedSourcesRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "LoadedSourcesRequest unsupported")
}

func (s *Server) onDataBreakpointInfoRequest(request *dap.DataBreakpointInfoRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "DataBreakpointInfoRequest unsupported")
}

func (s *Server) onSetDataBreakpointsRequest(request *dap.SetDataBreakpointsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "SetDataBreakpointsRequest unsupported")
}

func (s *Server) onReadMemoryRequest(request *dap.ReadMemoryRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "ReadMemoryRequest unsupported")
}

func (s *Server) onDisassembleRequest(request *dap.DisassembleRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "DisassembleRequest unsupported")
}

func (s *Server) onCancelRequest(request *dap.CancelRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "CancelRequest unsupported")
}

func (s *Server) onBreakpointLocationsRequest(request *dap.BreakpointLocationsRequest) {
	s.sendUnsupportedResponse(request.Seq, request.Command, "BreakpointLocationsRequest unsupported")
}

type outputWriter struct {
	s        *Server
	category string
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.s.send(&dap.OutputEvent{Event: *newEvent("output"), Body: dap.OutputEventBody{Category: w.category, Output: string(p)}})
	return len(p), nil
}

func newEvent(event string) *dap.Event {
	return &dap.Event{
		ProtocolMessage: dap.ProtocolMessage{
			Seq:  0,
			Type: "event",
		},
		Event: event,
	}
}

func newResponse(requestSeq int, command string) *dap.Response {
	return &dap.Response{
		ProtocolMessage: dap.ProtocolMessage{
			Seq:  0,
			Type: "response",
		},
		Command:    command,
		RequestSeq: requestSeq,
		Success:    true,
	}
}

func printWarnings(w io.Writer, warnings []client.VertexWarning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintf(w, "\n ")
	sb := &bytes.Buffer{}
	if len(warnings) == 1 {
		fmt.Fprintf(sb, "1 warning found")
	} else {
		fmt.Fprintf(sb, "%d warnings found", len(warnings))
	}
	fmt.Fprintf(sb, ":\n")

	for _, warn := range warnings {
		fmt.Fprintf(w, " - %s\n", warn.Short)
		for _, d := range warn.Detail {
			fmt.Fprintf(w, "%s\n", d)
		}
		if warn.URL != "" {
			fmt.Fprintf(w, "More info: %s\n", warn.URL)
		}
		if warn.SourceInfo != nil && warn.Range != nil {
			src := errdefs.Source{
				Info:   warn.SourceInfo,
				Ranges: warn.Range,
			}
			src.Print(w)
		}
		fmt.Fprintf(w, "\n")

	}
}

func listToMap(values []string, defaultEnv bool) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kv := strings.SplitN(value, "=", 2)
		if len(kv) == 1 {
			if defaultEnv {
				v, ok := os.LookupEnv(kv[0])
				if ok {
					result[kv[0]] = v
				}
			} else {
				result[kv[0]] = ""
			}
		} else {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

type stdioConn struct {
	io.Reader
	io.Writer
}

func (c *stdioConn) Read(b []byte) (n int, err error) {
	return c.Reader.Read(b)
}
func (c *stdioConn) Write(b []byte) (n int, err error) {
	return c.Writer.Write(b)
}
func (c *stdioConn) Close() error                       { return nil }
func (c *stdioConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *stdioConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *stdioConn) SetDeadline(t time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (a dummyAddr) Network() string { return "dummy" }
func (a dummyAddr) String() string  { return "dummy" }
