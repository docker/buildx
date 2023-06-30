package walker

import (
	"context"
	"sync"

	solverpb "github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
)

// Controller is a utility to control walkers with debugger-like interface like "continue" and "next".
type Controller struct {
	def         *solverpb.Definition
	breakpoints *Breakpoints

	breakHandler    BreakHandlerFunc
	onVertexHandler OnVertexHandlerFunc
	onWalkDoneFunc  func(error)

	walker     *Walker
	walkerMu   sync.Mutex
	walkCancel func()

	curStepDoneCh chan struct{}

	curWalkErrCh  chan error
	curWalkDoneCh chan struct{}
}

// Status is a status of the controller.
type Status struct {

	// Definition is the target definition where walking is performed.
	Definition *solverpb.Definition

	// Cursors is current cursor positions on the walker.
	Cursors []solverpb.Range
}

// NewController returns a walker controller.
func NewController(def *solverpb.Definition, breakpoints *Breakpoints, breakHandler BreakHandlerFunc, onVertexHandler OnVertexHandlerFunc, onWalkDoneFunc func(error)) *Controller {
	return &Controller{
		def:             def,
		breakpoints:     breakpoints,
		breakHandler:    breakHandler,
		onVertexHandler: onVertexHandler,
		onWalkDoneFunc:  onWalkDoneFunc,
	}
}

// Breakpoint returns a set of breakpoints currently recognized.
func (c *Controller) Breakpoints() *Breakpoints {
	return c.breakpoints
}

// Inspect returns the current status.
func (c *Controller) Inspect() *Status {
	c.walkerMu.Lock()
	defer c.walkerMu.Unlock()
	var cursors []solverpb.Range
	if c.walker != nil {
		cursors = c.walker.GetCursors()
	}
	return &Status{
		Definition: c.def,
		Cursors:    cursors,
	}
}

// IsStarted returns true when there is an on-going walker. Returns false otherwise.
func (c *Controller) IsStarted() bool {
	c.walkerMu.Lock()
	w := c.walker
	c.walkerMu.Unlock()
	return w != nil
}

// StartWalk starts walking in a gorouitne. This function returns immediately without waiting for the
// completion of the walking. Parallel invoking of this method isn't supported and an error will be returned
// if there is an on-going walker. Previous walking must be canceled using WalkCancel method.
func (c *Controller) StartWalk() error {
	c.walkerMu.Lock()
	if c.walker != nil {
		c.walkerMu.Unlock()
		return errors.Errorf("walker already running")
	}
	c.walkerMu.Unlock()

	go func() {
		w := NewWalker(c.breakpoints, func(ctx context.Context, bCtx *BreakContext) error {
			if err := c.breakHandler(ctx, bCtx); err != nil {
				return err
			}
			curStepDoneCh := make(chan struct{})
			c.curStepDoneCh = curStepDoneCh
			select {
			case <-curStepDoneCh:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}, c.onVertexHandler)

		c.walkerMu.Lock()
		c.walker = w
		c.walkerMu.Unlock()

		ctx, cancel := context.WithCancel(context.TODO())
		c.walkCancel = cancel
		c.curWalkErrCh = make(chan error)
		c.curWalkDoneCh = make(chan struct{})
		err := w.Walk(ctx, c.def)
		c.onWalkDoneFunc(err)
		w.Close()

		c.walkerMu.Lock()
		c.walker = nil
		c.walkerMu.Unlock()

		if err != nil {
			c.curWalkErrCh <- err
		}
		close(c.curWalkDoneCh)
	}()

	return nil
}

// WalkCancel cancels on-going walking.
func (c *Controller) WalkCancel() error {
	c.walkerMu.Lock()
	if c.walker != nil {
		c.walker = nil
		c.walkerMu.Unlock()
		c.walkCancel()
		select {
		case err := <-c.curWalkErrCh:
			return err
		case <-c.curWalkDoneCh:
		}
		return nil
	}
	c.walkerMu.Unlock()
	return nil
}

// Continue resumes the walker. The walker will stop at the next breakpoint.
func (c *Controller) Continue() {
	c.walkerMu.Lock()
	defer c.walkerMu.Unlock()
	if c.walker != nil {
		c.walker.BreakAllNode(false)
	}
	if c.curStepDoneCh != nil {
		close(c.curStepDoneCh)
		c.curStepDoneCh = nil
	}
}

// Next resumes the walker. The walker will stop at the next vertex.
func (c *Controller) Next() error {
	c.walkerMu.Lock()
	defer c.walkerMu.Unlock()
	if c.walker != nil {
		c.walker.BreakAllNode(true)
	} else {
		return errors.Errorf("walker isn't running")
	}
	if c.curStepDoneCh != nil {
		close(c.curStepDoneCh)
		c.curStepDoneCh = nil
	}
	return nil
}

// Close closes this controller.
func (c *Controller) Close() error {
	return c.WalkCancel()
}
