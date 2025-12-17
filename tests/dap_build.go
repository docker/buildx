package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/docker/buildx/commands"
	debug "github.com/docker/buildx/dap"
	"github.com/docker/buildx/dap/common"
	"github.com/docker/buildx/util/daptest"
	"github.com/google/go-dap"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dapBuildCmd(t *testing.T, sb integration.Sandbox, opts ...cmdOpt) (*daptest.Client, func(interrupt bool) error, error) {
	if !isExperimental() {
		t.Skip("only testing when experimental is enabled")
	}

	opts = append([]cmdOpt{withArgs("dap", "build")}, opts...)

	cmd := buildxCmd(sb, opts...)
	pr, err := cmd.StdinPipe()
	require.NoError(t, err)

	pw, err := cmd.StdoutPipe()
	require.NoError(t, err)

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	conn := daptest.LogConn(t, "client", debug.NewConn(pw, pr))
	client := daptest.NewClient(conn)

	done := func(interrupt bool) error {
		defer client.Close()

		if interrupt {
			t.Log("sending interrupt")
			signal := os.Interrupt
			if runtime.GOOS == "windows" {
				// Interrupt on windows is not implemented.
				signal = syscall.SIGTERM
			}
			cmd.Process.Signal(signal)
		}

		// Attempt to wait for the process first. In general, we want
		// the process to exit normally.
		//
		// If too much time passes when waiting, kill the command.
		timer := time.AfterFunc(10*time.Second, func() {
			t.Logf("killing process %v", cmd.Process.Pid)
			cmd.Process.Kill()
		})
		defer timer.Stop()

		t.Log("waiting for process to finish")
		defer t.Log("process exited")

		return cmd.Wait()
	}
	return client, done, nil
}

var dapBuildTests = []func(t *testing.T, sb integration.Sandbox){
	testDapBuild,
	testDapBuildStopOnEntry,
	testDapBuildSetBreakpoints,
	testDapBuildStepIn,
	testDapBuildStepNext,
	testDapBuildStepOut,
	testDapBuildVariables,
}

func testDapBuild(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb)
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
	})

	select {
	case <-time.After(10 * time.Second):
		require.Fail(t, "timeout reached")
	case em := <-interruptCh:
		require.Equal(t, "terminated", em.GetEvent().Event)
	}
	require.NoError(t, done(false))
}

func testDapBuildStopOnEntry(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb, withArgs(dir))
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
		Config: common.Config{
			StopOnEntry: true,
		},
	})

	stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
	threads := doThreads(t, client)
	require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

	stackTraceResp := <-daptest.DoRequest[*dap.StackTraceResponse](t, client, &dap.StackTraceRequest{
		Request: dap.Request{Command: "stackTrace"},
		Arguments: dap.StackTraceArguments{
			ThreadId: stopped.Body.ThreadId,
		},
	})
	require.True(t, stackTraceResp.Success)
	require.Len(t, stackTraceResp.Body.StackFrames, 1)

	var exitErr *exec.ExitError
	require.ErrorAs(t, done(true), &exitErr)
}

func testDapBuildSetBreakpoints(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb, withArgs(dir))
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
	},
		dap.SourceBreakpoint{Line: 2},
		dap.SourceBreakpoint{Line: 4},
	)

	stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
	require.NotNil(t, stopped)

	threads := doThreads(t, client)
	require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

	// Expect 2 stack frames. We should be stopped at line 2 which is reached
	// from within the copy.
	stackFrames := doStackTrace(t, client, stopped.Body.ThreadId)
	assertStackTrace(t, stackFrames, []stackFrameMatcher{
		{
			SourceName: "Dockerfile",
			Line:       2,
			Name:       `^\[base .*\] FROM`,
		},
		{
			SourceName: "Dockerfile",
			Line:       7,
			Name:       `^\[stage-1 .*\] COPY`,
		},
	})

	// Continue should stop at the next breakpoint.
	doContinue(t, client, stopped.Body.ThreadId)

	stopped = waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
	require.NotNil(t, stopped)

	threads = doThreads(t, client)
	require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

	stackFrames = doStackTrace(t, client, stopped.Body.ThreadId)
	assertStackTrace(t, stackFrames, []stackFrameMatcher{
		{
			SourceName: "Dockerfile",
			Line:       4,
			Name:       `^\[base .*\] RUN cp`,
		},
		{
			SourceName: "Dockerfile",
			Line:       7,
			Name:       `^\[stage-1 .*\] COPY`,
		},
	})

	// Continue should go until the program ends.
	doContinue(t, client, stopped.Body.ThreadId)

	require.NoError(t, done(false))
}

func testDapBuildStepIn(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb, withArgs(dir))
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
		Config: common.Config{
			StopOnEntry: true,
		},
	})

	expected := [][]stackFrameMatcher{
		// stop point 1
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 2
		{
			{
				SourceName: "Dockerfile",
				Line:       2,
				Name:       `^\[base .*\] FROM .*/busybox`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 3
		{
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// the following three steps are unintended and are the result
		// of a bug in the debug adapter.
		// see issue https://github.com/docker/buildx/issues/3565
		{
			{
				Name: `^\[internal\] load build context`,
			},
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// todo: this shouldn't be a stop point.
		{
			{
				Name: `^\[internal\] load build context`,
			},
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// duplicate of stop point 3 because of unintended branch
		// associated with the build context copy.
		{
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 4
		{
			{
				SourceName: "Dockerfile",
				Line:       4,
				Name:       `^\[base .*\] RUN cp /etc/foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 5
		// since we're at the end of a stage, the last stop point
		// repeats to allow inspecting the return state.
		{
			{
				SourceName: "Dockerfile",
				Line:       4,
				Name:       `^\[base .*\] RUN cp /etc/foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 6
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 7
		// repeat of stop point 5 but after the invocation.
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
	}

	for _, exp := range expected {
		stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
		require.NotNil(t, stopped)

		threads := doThreads(t, client)
		require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

		stackFrames := doStackTrace(t, client, stopped.Body.ThreadId)
		assertStackTrace(t, stackFrames, exp)

		doStepIn(t, client, stopped.Body.ThreadId)
	}

	require.NoError(t, done(false))
}

func testDapBuildStepNext(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb, withArgs(dir))
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
		Config: common.Config{
			StopOnEntry: true,
		},
	},
		dap.SourceBreakpoint{Line: 3},
	)

	expected := [][]stackFrameMatcher{
		// stop point 1
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 2
		// next would normally skip over base but we have a breakpoint
		// on this line and it should not be skipped over.
		{
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 3
		{
			{
				SourceName: "Dockerfile",
				Line:       4,
				Name:       `^\[base .*\] RUN cp /etc/foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 4
		// since we're at the end of a stage, the last stop point
		// repeats to allow inspecting the return state.
		{
			{
				SourceName: "Dockerfile",
				Line:       4,
				Name:       `^\[base .*\] RUN cp /etc/foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 5
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 6
		// repeat of stop point 5 but after the invocation.
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
	}

	for _, exp := range expected {
		stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
		require.NotNil(t, stopped)

		threads := doThreads(t, client)
		require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

		stackFrames := doStackTrace(t, client, stopped.Body.ThreadId)
		assertStackTrace(t, stackFrames, exp)

		doNext(t, client, stopped.Body.ThreadId)
	}

	require.NoError(t, done(false))
}

func testDapBuildStepOut(t *testing.T, sb integration.Sandbox) {
	dir := createTestProject(t)
	client, done, err := dapBuildCmd(t, sb, withArgs(dir))
	require.NoError(t, err)

	interruptCh := pollInterruptEvents(client)
	doLaunch(t, client, commands.LaunchConfig{
		Dockerfile:  path.Join(dir, "Dockerfile"),
		ContextPath: dir,
		Config: common.Config{
			StopOnEntry: true,
		},
	},
		dap.SourceBreakpoint{Line: 3},
	)

	expected := [][]stackFrameMatcher{
		// stop point 1
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 2
		// out would normally skip over base but we have a breakpoint
		// on this line and it should not be skipped over.
		{
			{
				SourceName: "Dockerfile",
				Line:       3,
				Name:       `^\[base .*\] COPY foo`,
			},
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 3
		{
			{
				SourceName: "Dockerfile",
				Line:       7,
				Name:       `^\[stage-1 .*\] COPY .* /etc/bar`,
			},
		},
		// stop point 3 should not be repeated unlike the
		// previous methods because step out will skip
		// the duplicate last step.
	}

	for _, exp := range expected {
		stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
		require.NotNil(t, stopped)

		threads := doThreads(t, client)
		require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

		stackFrames := doStackTrace(t, client, stopped.Body.ThreadId)
		assertStackTrace(t, stackFrames, exp)

		doStepOut(t, client, stopped.Body.ThreadId)
	}

	require.NoError(t, done(false))
}

func testDapBuildVariables(t *testing.T, sb integration.Sandbox) {
	tests := []struct {
		Name       string
		Breakpoint dap.SourceBreakpoint
		Expected   []variableScopeMatcher
	}{
		{
			Name:       "FROM",
			Breakpoint: dap.SourceBreakpoint{Line: 2},
			Expected: []variableScopeMatcher{
				{
					Name:             "Arguments",
					PresentationHint: "arguments",
					Expensive:        false,
					Variables: variableSetMatcher{
						Variables: []variableMatcher{
							{
								Name:  "platform",
								Value: `^(.*)/(.*)$`,
								Nested: &variableSetMatcher{
									Variables: []variableMatcher{
										{
											Name:  "architecture",
											Value: "^[^/]*$",
										},
										{
											Name:  "os",
											Value: "^[^/]*$",
										},
									},
									NonExhaustive: true,
								},
							},
						},
					},
				},
			},
		},
		{
			Name:       "COPY",
			Breakpoint: dap.SourceBreakpoint{Line: 3},
			Expected: []variableScopeMatcher{
				{
					Name:             "Arguments",
					PresentationHint: "arguments",
					Expensive:        false,
					Variables: variableSetMatcher{
						Variables: []variableMatcher{},
					},
				},
				{
					Name:             "File Explorer",
					PresentationHint: "locals",
					Expensive:        true,
					Variables: variableSetMatcher{
						// Do not check the variables in the file explorer since
						// the underlying image may change.
						NonExhaustive: true,
					},
				},
			},
		},
		{
			Name:       "RUN",
			Breakpoint: dap.SourceBreakpoint{Line: 4},
			Expected: []variableScopeMatcher{
				{
					Name:             "Arguments",
					PresentationHint: "arguments",
					Expensive:        false,
					Variables: variableSetMatcher{
						Variables: []variableMatcher{
							{
								Name:  "platform",
								Value: `^(.*)/(.*)$`,
								Nested: &variableSetMatcher{
									Variables: []variableMatcher{
										{
											Name:  "architecture",
											Value: "^[^/]*$",
										},
										{
											Name:  "os",
											Value: "^[^/]*$",
										},
									},
									NonExhaustive: true,
								},
							},
							{
								Name:  "exec",
								Value: `/bin/sh -c cp /etc/foo /etc/bar`,
								Nested: &variableSetMatcher{
									Variables: []variableMatcher{
										{
											Name:  "args",
											Value: `/bin/sh -c cp /etc/foo /etc/bar`,
											Nested: &variableSetMatcher{
												Variables: []variableMatcher{
													{
														Name:  "0",
														Value: "/bin/sh",
													},
													{
														Name:  "1",
														Value: "-c",
													},
													{
														Name:  "2",
														Value: "cp /etc/foo /etc/bar",
													},
												},
											},
										},
										{
											Name:  "env",
											Value: `.*`,
											Nested: &variableSetMatcher{
												Variables: []variableMatcher{
													{
														Name:  "PATH",
														Value: `.*`,
													},
												},
												NonExhaustive: true,
											},
										},
										{
											Name:  "workdir",
											Value: "/",
										},
									},
								},
							},
						},
					},
				},
				{
					Name:             "File Explorer",
					PresentationHint: "locals",
					Expensive:        true,
					Variables: variableSetMatcher{
						// Do not check the variables in the file explorer since
						// the underlying image may change.
						NonExhaustive: true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := createTestProject(t)
			client, done, err := dapBuildCmd(t, sb)
			require.NoError(t, err)

			interruptCh := pollInterruptEvents(client)
			doLaunch(t, client, commands.LaunchConfig{
				Dockerfile:  path.Join(dir, "Dockerfile"),
				ContextPath: dir,
			}, tt.Breakpoint)

			stopped := waitForInterrupt[*dap.StoppedEvent](t, interruptCh)
			threads := doThreads(t, client)
			require.ElementsMatch(t, []int{stopped.Body.ThreadId}, threads)

			// Only check the first stack frame.
			stackFrames := doStackTrace(t, client, stopped.Body.ThreadId)
			require.GreaterOrEqual(t, len(stackFrames), 1)

			scopes := doScopes(t, client, stackFrames[0].Id)
			assertVariableScopes(t, client, scopes, tt.Expected)

			var exitErr *exec.ExitError
			require.ErrorAs(t, done(true), &exitErr)
		})
	}
}

func doLaunch(t *testing.T, client *daptest.Client, config commands.LaunchConfig, bps ...dap.SourceBreakpoint) {
	t.Helper()

	configurationDoneCh := make(chan (<-chan *dap.ConfigurationDoneResponse))
	client.RegisterEvent("initialized", func(em dap.EventMessage) {
		go func() {
			if len(bps) > 0 {
				setBreakpointsResp := <-daptest.DoRequest[*dap.SetBreakpointsResponse](t, client, &dap.SetBreakpointsRequest{
					Request: dap.Request{Command: "setBreakpoints"},
					Arguments: dap.SetBreakpointsArguments{
						Source: dap.Source{
							Name: path.Base(config.Dockerfile),
							Path: config.Dockerfile,
						},
						Breakpoints: bps,
					},
				})
				assert.True(t, setBreakpointsResp.Success)
			}

			// Send configuration done since we don't do any configuration.
			configurationDoneCh <- daptest.DoRequest[*dap.ConfigurationDoneResponse](t, client, &dap.ConfigurationDoneRequest{
				Request: dap.Request{Command: "configurationDone"},
			})
		}()
	})

	initializeResp := <-daptest.DoRequest[*dap.InitializeResponse](t, client, &dap.InitializeRequest{
		Request: dap.Request{Command: "initialize"},
	})
	require.True(t, initializeResp.Success)
	require.True(t, initializeResp.Body.SupportsConfigurationDoneRequest)

	args, err := json.Marshal(config)
	require.NoError(t, err)

	launchResp := <-daptest.DoRequest[*dap.LaunchResponse](t, client, &dap.LaunchRequest{
		Request:   dap.Request{Command: "launch"},
		Arguments: json.RawMessage(args),
	})
	require.True(t, launchResp.Success)

	var configurationDone <-chan *dap.ConfigurationDoneResponse
	select {
	case configurationDone = <-configurationDoneCh:
	case <-time.After(10 * time.Second):
		require.Fail(t, "timeout reached")
	}

	configurationDoneResp := <-configurationDone
	require.True(t, configurationDoneResp.Success)
}

func doStepIn(t *testing.T, client *daptest.Client, threadID int) {
	t.Helper()

	stepResp := <-daptest.DoRequest[*dap.StepInResponse](t, client, &dap.StepInRequest{
		Request: dap.Request{Command: "stepIn"},
		Arguments: dap.StepInArguments{
			ThreadId: threadID,
		},
	})
	assert.True(t, stepResp.Success)
}

func doNext(t *testing.T, client *daptest.Client, threadID int) {
	t.Helper()

	stepResp := <-daptest.DoRequest[*dap.NextResponse](t, client, &dap.NextRequest{
		Request: dap.Request{Command: "next"},
		Arguments: dap.NextArguments{
			ThreadId: threadID,
		},
	})
	assert.True(t, stepResp.Success)
}

func doStepOut(t *testing.T, client *daptest.Client, threadID int) {
	t.Helper()

	stepResp := <-daptest.DoRequest[*dap.StepOutResponse](t, client, &dap.StepOutRequest{
		Request: dap.Request{Command: "stepOut"},
		Arguments: dap.StepOutArguments{
			ThreadId: threadID,
		},
	})
	assert.True(t, stepResp.Success)
}

func doContinue(t *testing.T, client *daptest.Client, threadID int) {
	t.Helper()

	continueResp := <-daptest.DoRequest[*dap.ContinueResponse](t, client, &dap.ContinueRequest{
		Request: dap.Request{Command: "continue"},
		Arguments: dap.ContinueArguments{
			ThreadId: threadID,
		},
	})
	assert.True(t, continueResp.Success)
}

func doThreads(t *testing.T, client *daptest.Client) []int {
	t.Helper()

	threadsResp := <-daptest.DoRequest[*dap.ThreadsResponse](t, client, &dap.ThreadsRequest{
		Request: dap.Request{Command: "threads"},
	})
	require.True(t, threadsResp.Success)

	ids := make([]int, 0, len(threadsResp.Body.Threads))
	for _, thread := range threadsResp.Body.Threads {
		ids = append(ids, thread.Id)
	}
	return ids
}

func doStackTrace(t *testing.T, client *daptest.Client, threadID int) []dap.StackFrame {
	t.Helper()

	stackTraceResp := <-daptest.DoRequest[*dap.StackTraceResponse](t, client, &dap.StackTraceRequest{
		Request: dap.Request{Command: "stackTrace"},
		Arguments: dap.StackTraceArguments{
			ThreadId: threadID,
		},
	})
	require.True(t, stackTraceResp.Success)

	return stackTraceResp.Body.StackFrames
}

func doScopes(t *testing.T, client *daptest.Client, frameID int) []dap.Scope {
	t.Helper()

	scopesResp := <-daptest.DoRequest[*dap.ScopesResponse](t, client, &dap.ScopesRequest{
		Request: dap.Request{Command: "scopes"},
		Arguments: dap.ScopesArguments{
			FrameId: frameID,
		},
	})
	require.True(t, scopesResp.Success)

	return scopesResp.Body.Scopes
}

func doVariables(t *testing.T, client *daptest.Client, referenceID int) []dap.Variable {
	t.Helper()

	variablesResp := <-daptest.DoRequest[*dap.VariablesResponse](t, client, &dap.VariablesRequest{
		Request: dap.Request{Command: "variables"},
		Arguments: dap.VariablesArguments{
			VariablesReference: referenceID,
		},
	})
	require.True(t, variablesResp.Success)

	return variablesResp.Body.Variables
}

func pollInterruptEvents(client *daptest.Client) <-chan dap.EventMessage {
	// Extra space in the message queue so unread events don't
	// cause the client to hang.
	ch := make(chan dap.EventMessage, 10)
	client.RegisterEvent("stopped", func(em dap.EventMessage) {
		ch <- em
	})

	client.RegisterEvent("terminated", func(em dap.EventMessage) {
		ch <- em
	})
	return ch
}

func waitForInterrupt[E dap.EventMessage](t *testing.T, interruptCh <-chan dap.EventMessage) (e E) {
	t.Helper()

	select {
	case <-time.After(10 * time.Second):
		require.Fail(t, "timeout reached")
	case em := <-interruptCh:
		require.IsType(t, e, em)
		e, _ = em.(E)
	}
	return e
}

type stackFrameMatcher struct {
	SourceName string
	Line       int
	Name       any
}

func (m *stackFrameMatcher) AssertMatches(t *testing.T, actual *dap.StackFrame) {
	t.Helper()

	var actualName string
	if actual.Source != nil {
		actualName = actual.Source.Name
	}
	assert.Equal(t, m.Line, actual.Line)
	assert.Equal(t, m.SourceName, actualName)
	assert.Regexp(t, m.Name, actual.Name)
}

func assertStackTrace(t *testing.T, actual []dap.StackFrame, expected []stackFrameMatcher) {
	t.Helper()

	if assert.Len(t, actual, len(expected)) {
		for i, exp := range expected {
			exp.AssertMatches(t, &actual[i])
		}
	}
}

type variableScopeMatcher struct {
	Name             string
	PresentationHint string
	Expensive        bool
	Variables        variableSetMatcher
}

func assertVariableScopes(t *testing.T, client *daptest.Client, actual []dap.Scope, expected []variableScopeMatcher) {
	t.Helper()

	assert.Len(t, actual, len(expected))
	for _, m := range expected {
		index := slices.IndexFunc(actual, func(o dap.Scope) bool {
			return m.Name == o.Name
		})

		if assert.GreaterOrEqualf(t, index, 0, "no scope with name %q", m.Name) {
			act := &actual[index]
			assert.Equal(t, m.PresentationHint, act.PresentationHint)
			assert.Equal(t, m.Expensive, act.Expensive)
			assertVariableSet(t, client, act.VariablesReference, &m.Variables)
		}
	}
}

type variableSetMatcher struct {
	// Variables covers variables inside this variable set.
	// Variables can be in any order.
	Variables []variableMatcher

	// NonExhaustive defines if this matcher is non-exhaustive.
	// A non-exhaustive matcher will just check for the existence
	// of the variables listed and won't check if there are extra
	// variables.
	NonExhaustive bool
}

type variableMatcher struct {
	Name   string
	Value  any
	Nested *variableSetMatcher
}

func (m *variableSetMatcher) AssertMatches(t *testing.T, client *daptest.Client, actual []dap.Variable) {
	t.Helper()

	if !m.NonExhaustive {
		assert.Len(t, actual, len(m.Variables))
	}

	for _, v := range m.Variables {
		index := slices.IndexFunc(actual, func(o dap.Variable) bool {
			return v.Name == o.Name
		})

		if assert.GreaterOrEqualf(t, index, 0, "no variable with name %q", v.Name) {
			act := &actual[index]
			assert.Regexp(t, v.Value, act.Value)
			assertVariableSet(t, client, act.VariablesReference, v.Nested)
		}
	}
}

func assertVariableSet(t *testing.T, client *daptest.Client, referenceID int, expected *variableSetMatcher) {
	t.Helper()

	if expected == nil {
		assert.LessOrEqual(t, referenceID, 0)
		return
	}

	if assert.Greater(t, referenceID, 0) {
		variables := doVariables(t, client, referenceID)
		expected.AssertMatches(t, client, variables)
	}
}
