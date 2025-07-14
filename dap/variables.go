package dap

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/go-dap"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/solver/pb"
)

type frame struct {
	dap.StackFrame
	scopes []dap.Scope
}

func (f *frame) setNameFromMeta(meta llb.OpMetadata) {
	if name, ok := meta.Description["llb.customname"]; ok {
		f.Name = name
	} else if cmd, ok := meta.Description["com.docker.dockerfile.v1.command"]; ok {
		f.Name = cmd
	}
	// TODO: should we infer the name from somewhere else?
}

func (f *frame) fillLocation(def *llb.Definition, loc *pb.Locations, ws string) {
	for _, l := range loc.Locations {
		for _, r := range l.Ranges {
			f.Line = int(r.Start.Line)
			f.Column = int(r.Start.Character)
			f.EndLine = int(r.End.Line)
			f.EndColumn = int(r.End.Character)

			info := def.Source.Infos[l.SourceIndex]
			f.Source = &dap.Source{
				Path: filepath.Join(ws, info.Filename),
			}
			return
		}
	}
}

func (f *frame) fillVarsFromOp(op *pb.Op, refs *variableReferences) {
	f.scopes = []dap.Scope{
		{
			Name:             "Arguments",
			PresentationHint: "arguments",
			VariablesReference: refs.New(func() []dap.Variable {
				var vars []dap.Variable
				if op.Platform != nil {
					vars = append(vars, platformVars(op.Platform, refs))
				}

				switch op := op.Op.(type) {
				case *pb.Op_Exec:
					vars = append(vars, execOpVars(op.Exec, refs))
				}
				return vars
			}),
		},
	}
}

func platformVars(platform *pb.Platform, refs *variableReferences) dap.Variable {
	return dap.Variable{
		Name:  "platform",
		Value: fmt.Sprintf("%s/%s", platform.OS, platform.Architecture),
		VariablesReference: refs.New(func() []dap.Variable {
			vars := []dap.Variable{
				{
					Name:  "architecture",
					Value: platform.Architecture,
				},
				{
					Name:  "os",
					Value: platform.OS,
				},
			}

			if platform.Variant != "" {
				vars = append(vars, dap.Variable{
					Name:  "variant",
					Value: platform.Variant,
				})
			}

			if platform.OSVersion != "" {
				vars = append(vars, dap.Variable{
					Name:  "osversion",
					Value: platform.OSVersion,
				})
			}
			return vars
		}),
	}
}

func execOpVars(exec *pb.ExecOp, refs *variableReferences) dap.Variable {
	return dap.Variable{
		Name:  "exec",
		Value: strings.Join(exec.Meta.Args, " "),
		VariablesReference: refs.New(func() []dap.Variable {
			vars := []dap.Variable{
				{
					Name:  "args",
					Value: brief(strings.Join(exec.Meta.Args, " ")),
					VariablesReference: refs.New(func() []dap.Variable {
						vars := make([]dap.Variable, 0, len(exec.Meta.Args))
						for i, arg := range exec.Meta.Args {
							vars = append(vars, dap.Variable{
								Name:  strconv.Itoa(i),
								Value: arg,
							})
						}
						return vars
					}),
				},
				{
					Name:  "env",
					Value: brief(strings.Join(exec.Meta.Env, " ")),
					VariablesReference: refs.New(func() []dap.Variable {
						vars := make([]dap.Variable, 0, len(exec.Meta.Env))
						for _, envstr := range exec.Meta.Env {
							parts := strings.SplitN(envstr, "=", 2)
							vars = append(vars, dap.Variable{
								Name:  parts[0],
								Value: parts[1],
							})
						}
						return vars
					}),
				},
			}

			if exec.Meta.Cwd != "" {
				vars = append(vars, dap.Variable{
					Name:  "workdir",
					Value: exec.Meta.Cwd,
				})
			}

			if exec.Meta.User != "" {
				vars = append(vars, dap.Variable{
					Name:  "user",
					Value: exec.Meta.User,
				})
			}
			return vars
		}),
	}
}

func (f *frame) Scopes() []dap.Scope {
	return f.scopes
}

type variableReferences struct {
	refs   map[int32]func() []dap.Variable
	nextID atomic.Int32
	mask   int32

	mu sync.RWMutex
}

func newVariableReferences() *variableReferences {
	v := new(variableReferences)
	v.Reset()
	return v
}

func (v *variableReferences) New(fn func() []dap.Variable) int {
	v.mu.Lock()
	defer v.mu.Unlock()

	id := v.nextID.Add(1) | v.mask
	v.refs[id] = sync.OnceValue(fn)
	return int(id)
}

func (v *variableReferences) Get(id int) []dap.Variable {
	v.mu.RLock()
	fn := v.refs[int32(id)]
	v.mu.RUnlock()

	var vars []dap.Variable
	if fn != nil {
		vars = fn()
	}

	if vars == nil {
		vars = []dap.Variable{}
	}
	return vars
}

func (v *variableReferences) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.refs = make(map[int32]func() []dap.Variable)
	v.nextID.Store(0)
}

func brief(s string) string {
	if len(s) >= 64 {
		return s[:60] + " ..."
	}
	return s
}
