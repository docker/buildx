package bake

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
)

func ParseHCLFile(dt []byte, fn string) (*hcl.File, bool, error) {
	var err error
	if strings.HasSuffix(fn, ".json") {
		f, diags := hclparse.NewParser().ParseJSON(dt, fn)
		if diags.HasErrors() {
			err = diags
		}
		return f, true, err
	}
	if strings.HasSuffix(fn, ".hcl") {
		f, diags := hclparse.NewParser().ParseHCL(dt, fn)
		if diags.HasErrors() {
			err = diags
		}
		return f, true, err
	}
	f, diags := hclparse.NewParser().ParseHCL(dt, fn+".hcl")
	if diags.HasErrors() {
		f, diags2 := hclparse.NewParser().ParseJSON(dt, fn+".json")
		if !diags2.HasErrors() {
			return f, true, nil
		}
		return nil, false, diags
	}
	return f, true, nil
}

func formatHCLError(err error, files []File) error {
	if err == nil {
		return nil
	}
	diags, ok := err.(hcl.Diagnostics)
	if !ok {
		return err
	}
	for _, d := range diags {
		if d.Severity != hcl.DiagError {
			continue
		}
		if d.Subject != nil {
			var dt []byte
			for _, f := range files {
				if d.Subject.Filename == f.Name {
					dt = f.Data
					break
				}
			}
			src := &errdefs.Source{
				Info: &pb.SourceInfo{
					Filename: d.Subject.Filename,
					Data:     dt,
				},
				Ranges: []*pb.Range{toErrRange(d.Subject)},
			}
			err = errdefs.WithSource(err, src)
			break
		}
	}
	return err
}

func toErrRange(in *hcl.Range) *pb.Range {
	return &pb.Range{
		Start: &pb.Position{Line: int32(in.Start.Line), Character: int32(in.Start.Column)},
		End:   &pb.Position{Line: int32(in.End.Line), Character: int32(in.End.Column)},
	}
}
