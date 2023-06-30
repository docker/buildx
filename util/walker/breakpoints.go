package walker

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/moby/buildkit/client/llb"
	solverpb "github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
)

// Breakpoint represents a breakpoint.
type Breakpoint interface {
	IsTarget(ctx context.Context, st llb.State, contErr error) (yes bool, hitLocations []*solverpb.Range, err error)
	IsMarked(line int64) bool
	String() string
	Init()
}

// Breakpoints manages a set of breakpoints.
type Breakpoints struct {
	isBreakAllNode atomic.Bool

	breakpoints   map[string]Breakpoint
	breakpointsMu sync.Mutex

	breakpointIdx int64
}

// NewBreakpoints returns an empty set of breakpoints.
func NewBreakpoints() *Breakpoints {
	return &Breakpoints{}
}

// Add adds a breakpoint with the specified key.
func (b *Breakpoints) Add(key string, bp Breakpoint) (string, error) {
	b.breakpointsMu.Lock()
	defer b.breakpointsMu.Unlock()
	if b.breakpoints == nil {
		b.breakpoints = make(map[string]Breakpoint)
	}
	if key == "" {
		key = fmt.Sprintf("%d", atomic.AddInt64(&b.breakpointIdx, 1))
	}
	if _, ok := b.breakpoints[key]; ok {
		return "", errors.Errorf("breakpoint %q already exists: %v", key, b)
	}
	b.breakpoints[key] = bp
	return key, nil
}

// Clear removes the specified breakpoint.
func (b *Breakpoints) Clear(key string) {
	b.breakpointsMu.Lock()
	defer b.breakpointsMu.Unlock()
	delete(b.breakpoints, key)
}

// ClearAll removes all breakpoints.
func (b *Breakpoints) ClearAll() {
	b.breakpointsMu.Lock()
	defer b.breakpointsMu.Unlock()
	b.breakpoints = nil
	atomic.StoreInt64(&b.breakpointIdx, 0)
}

// ForEach calls the callback on each breakpoint.
func (b *Breakpoints) ForEach(f func(key string, bp Breakpoint) bool) {
	var keys []string
	for k := range b.breakpoints {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !f(k, b.breakpoints[k]) {
			return
		}
	}
}

// BreakAllNode enables to configure to break on each node.
func (b *Breakpoints) BreakAllNode(v bool) {
	b.isBreakAllNode.Store(v)
}

func (b *Breakpoints) isBreakpoint(ctx context.Context, st llb.State, handleErr error) (bool, map[string][]*solverpb.Range, error) {
	if b.isBreakAllNode.Load() {
		return true, nil, nil
	}

	b.breakpointsMu.Lock()
	defer b.breakpointsMu.Unlock()
	hits := make(map[string][]*solverpb.Range)
	for k, bp := range b.breakpoints {
		isBreak, bhits, err := bp.IsTarget(ctx, st, handleErr)
		if err != nil {
			return false, nil, err
		}
		if isBreak {
			hits[k] = append(hits[k], bhits...)
		}
	}
	if len(hits) > 0 {
		return true, hits, nil
	}
	return false, nil, nil
}

// NewLineBreakpoint returns a breakpoint to break on the specified line.
func NewLineBreakpoint(line int64) Breakpoint {
	return &lineBreakpoint{line}
}

type lineBreakpoint struct {
	line int64
}

func (b *lineBreakpoint) Init() {}

func (b *lineBreakpoint) IsTarget(ctx context.Context, st llb.State, _ error) (yes bool, hitLocations []*solverpb.Range, err error) {
	_, _, _, sources, err := st.Output().Vertex(ctx, nil).Marshal(ctx, nil)
	if err != nil {
		return false, nil, err
	}
	hits := make(map[solverpb.Range]struct{})
	line := b.line
	for _, loc := range sources {
		for _, r := range loc.Ranges {
			if int64(r.Start.Line) <= line && line <= int64(r.End.Line) {
				hits[*r] = struct{}{}
			}
		}
	}
	if len(hits) > 0 {
		var ret []*solverpb.Range
		for r := range hits {
			ret = append(ret, &r)
		}
		return true, ret, nil
	}
	return false, nil, nil
}

func (b *lineBreakpoint) IsMarked(line int64) bool {
	return line == b.line
}

func (b *lineBreakpoint) String() string {
	return fmt.Sprintf("line: %d", b.line)
}

// NewStopOnEntryBreakpoint returns a breakpoint that breaks at the first node.
func NewStopOnEntryBreakpoint() Breakpoint {
	b := stopOnEntryBreakpoint(true)
	return &b
}

type stopOnEntryBreakpoint bool

func (b *stopOnEntryBreakpoint) Init() {
	*b = true
}

func (b *stopOnEntryBreakpoint) IsTarget(ctx context.Context, st llb.State, _ error) (yes bool, hitLocations []*solverpb.Range, err error) {
	if *b {
		*b = false // stop only once
		return true, nil, nil
	}
	return false, nil, nil
}

func (b *stopOnEntryBreakpoint) IsMarked(line int64) bool {
	return false
}

func (b *stopOnEntryBreakpoint) String() string {
	return fmt.Sprintf("stop on entry")
}

// NewOnErrorBreakpoint returns a breakpoint that breaks when an error observed.
func NewOnErrorBreakpoint() Breakpoint {
	return &onErrorBreakpoint{}
}

type onErrorBreakpoint struct{}

func (b *onErrorBreakpoint) Init() {}

func (b *onErrorBreakpoint) IsTarget(ctx context.Context, st llb.State, handleErr error) (yes bool, hitLocations []*solverpb.Range, err error) {
	if handleErr == nil {
		return false, nil, nil
	}
	_, _, _, sources, err := st.Output().Vertex(ctx, nil).Marshal(ctx, nil)
	if err != nil {
		return false, nil, err
	}
	hits := make(map[solverpb.Range]struct{})
	for _, loc := range sources {
		for _, r := range loc.Ranges {
			hits[*r] = struct{}{}
		}
	}
	var ret []*solverpb.Range
	if len(hits) > 0 {
		for r := range hits {
			ret = append(ret, &r)
		}
	}
	return true, ret, nil
}

func (b *onErrorBreakpoint) IsMarked(line int64) bool {
	return false
}

func (b *onErrorBreakpoint) String() string {
	return fmt.Sprintf("stop on error")
}
