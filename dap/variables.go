package dap

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/go-dap"
	"github.com/moby/buildkit/client/llb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"
	"github.com/tonistiigi/fsutil/types"
)

type frame struct {
	dap.StackFrame
	op     *pb.Op
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

func (f *frame) fillLocation(def *llb.Definition, loc *pb.Locations, ws string, next *step) {
	for _, l := range loc.Locations {
		for _, r := range l.Ranges {
			if next != nil && f.Line != 0 {
				// We have location information. See if the new location
				// information matches with our location better.
				if !betterLocation(r, f, next) {
					continue
				}
			}

			f.Line = int(r.Start.Line)
			f.Column = int(r.Start.Character)
			f.EndLine = int(r.End.Line)
			f.EndColumn = int(r.End.Character)

			info := def.Source.Infos[l.SourceIndex]
			f.Source = &dap.Source{
				Name: path.Base(info.Filename),
				Path: filepath.Join(ws, info.Filename),
			}

			// If we do not have a next operation, then we don't have
			// any information to make a determination about the "best" fit
			// that happens at the beginning of this section. Exit early.
			if next == nil {
				return
			}
		}
	}
}

func (f *frame) ExportVars(ctx context.Context, mounts map[string]gateway.Reference, refs *variableReferences) {
	f.fillVarsFromOp(f.op, refs)
	if len(mounts) > 0 {
		f.fillVarsFromResult(ctx, mounts, refs)
	}
}

func (f *frame) ResetVars() {
	f.scopes = nil
}

func (f *frame) fillVarsFromOp(op *pb.Op, refs *variableReferences) {
	f.scopes = append(f.scopes, dap.Scope{
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
	})
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

func (f *frame) fillVarsFromResult(ctx context.Context, mounts map[string]gateway.Reference, refs *variableReferences) {
	f.scopes = append(f.scopes, dap.Scope{
		Name:             "File Explorer",
		PresentationHint: "locals",
		VariablesReference: refs.New(func() []dap.Variable {
			return fsVars(ctx, mounts, "/", refs)
		}),
		Expensive: true,
	})
}

func fsVars(ctx context.Context, mounts map[string]gateway.Reference, path string, vars *variableReferences) []dap.Variable {
	path, ref := lookupPath(path, mounts)
	if ref == nil {
		return nil
	}

	files, err := ref.ReadDir(ctx, gateway.ReadDirRequest{
		Path: path,
	})
	if err != nil {
		return []dap.Variable{
			{
				Name:  "error",
				Value: err.Error(),
			},
		}
	}

	paths := make([]dap.Variable, len(files))
	for i, file := range files {
		stat := statf(file)
		fv := dap.Variable{
			Name: file.Path,
		}

		fullpath := filepath.ToSlash(filepath.Join(path, file.Path))
		if file.IsDir() {
			fv.Name += "/"
			fv.VariablesReference = vars.New(func() []dap.Variable {
				dvar := dap.Variable{
					Name:  ".",
					Value: statf(file),
					VariablesReference: vars.New(func() []dap.Variable {
						return statVars(file)
					}),
				}
				return append([]dap.Variable{dvar}, fsVars(ctx, mounts, fullpath, vars)...)
			})
			fv.Value = ""
		} else {
			fv.Value = stat
			fv.VariablesReference = vars.New(func() (dvars []dap.Variable) {
				if fs.FileMode(file.Mode).IsRegular() {
					// Regular file so display a small blurb of the file.
					dvars = append(dvars, fileVars(ctx, ref, fullpath)...)
				}
				return append(dvars, statVars(file)...)
			})
		}
		paths[i] = fv
	}
	return paths
}

func statf(st *types.Stat) string {
	mode := fs.FileMode(st.Mode)
	modTime := time.Unix(0, st.ModTime).UTC()
	return fmt.Sprintf("%s %d:%d %s", mode, st.Uid, st.Gid, modTime.Format("Jan 2 15:04:05 2006"))
}

func fileVars(ctx context.Context, ref gateway.Reference, fullpath string) []dap.Variable {
	b, err := ref.ReadFile(ctx, gateway.ReadRequest{
		Filename: fullpath,
		Range:    &gateway.FileRange{Length: 512},
	})

	var (
		data    string
		dataErr error
	)
	if err != nil {
		data = err.Error()
	} else if isBinaryData(b) {
		data = "binary data"
	} else {
		if len(b) == 512 {
			// Get the remainder of the file.
			remaining, err := ref.ReadFile(ctx, gateway.ReadRequest{
				Filename: fullpath,
				Range:    &gateway.FileRange{Offset: 512},
			})
			if err != nil {
				dataErr = err
			} else {
				b = append(b, remaining...)
			}
		}
		data = string(b)
	}

	dvars := []dap.Variable{
		{
			Name:  "data",
			Value: data,
		},
	}
	if dataErr != nil {
		dvars = append(dvars, dap.Variable{
			Name:  "dataError",
			Value: dataErr.Error(),
		})
	}
	return dvars
}

func statVars(st *types.Stat) (vars []dap.Variable) {
	if st.Linkname != "" {
		vars = append(vars, dap.Variable{
			Name:  "linkname",
			Value: st.Linkname,
		})
	}

	mode := fs.FileMode(st.Mode)
	modTime := time.Unix(0, st.ModTime).UTC()
	vars = append(vars, []dap.Variable{
		{
			Name:  "mode",
			Value: mode.String(),
		},
		{
			Name:  "uid",
			Value: strconv.FormatUint(uint64(st.Uid), 10),
		},
		{
			Name:  "gid",
			Value: strconv.FormatUint(uint64(st.Gid), 10),
		},
		{
			Name:  "mtime",
			Value: modTime.Format("Jan 2 15:04:05 2006"),
		},
	}...)
	return vars
}

func (f *frame) Scopes() []dap.Scope {
	if f.scopes == nil {
		return []dap.Scope{}
	}
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

// isBinaryData uses heuristics to determine if the file
// is binary. Algorithm taken from this blog post:
// https://eli.thegreenplace.net/2011/10/19/perls-guess-if-file-is-text-or-binary-implemented-in-python/
func isBinaryData(b []byte) bool {
	odd := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0 {
			return true
		}

		isHighBit := c&128 > 0
		if !isHighBit {
			if c < 32 && c != '\n' && c != '\t' {
				odd++
			}
		} else {
			r, sz := utf8.DecodeRune(b)
			if r != utf8.RuneError && sz > 1 {
				i += sz - 1
				continue
			}
			odd++
		}
	}
	return float64(odd)/float64(len(b)) > .3
}

func brief(s string) string {
	if len(s) >= 64 {
		return s[:60] + " ..."
	}
	return s
}

func betterLocation(r *pb.Range, f *frame, next *step) bool {
	// Ideal guess is one that is before the next frame.
	if int(r.Start.Line) <= next.frame.Line {
		// And is later than our current guess.
		if int(r.Start.Line) > f.Line {
			return true
		}
	}

	// We're after the next frame so this is a bad guess.
	// Was our original one even worse?
	if int(r.Start.Line) < f.Line {
		// Yes it was. We'll consider this a better location.
		return true
	}

	// Doesn't seem to be a better location.
	return false
}

func lookupPath(path string, mounts map[string]gateway.Reference) (remainder string, ref gateway.Reference) {
	var prefix string
	for p, r := range mounts {
		if len(p) > len(prefix) && strings.HasPrefix(path, p) {
			prefix = p
			remainder, _ = filepath.Rel(prefix, p)
			ref = r
		}
	}
	return "/" + remainder, ref
}
