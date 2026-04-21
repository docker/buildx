package replay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

const (
	EventTypeNone               = ""
	EventTypeDescriptorMismatch = "DescriptorMismatch"
	EventTypeIndexBlobMismatch  = "IndexBlobMismatch"
	EventTypeConfigBlobMismatch = "ConfigBlobMismatch"
	EventTypeLayerBlobMismatch  = "LayerBlobMismatch"
)

// CompareEventInput carries the relevant object for one side of a mismatch.
// This intentionally stays small and demo-focused; TODO: re-evaluate diffoci
// once it no longer depends on older containerd/linkname behavior.
type CompareEventInput struct {
	Descriptor *ocispecs.Descriptor `json:"descriptor,omitempty"`
	Index      *ocispecs.Index      `json:"index,omitempty"`
	Manifest   *ocispecs.Manifest   `json:"manifest,omitempty"`
}

// CompareEvent records a single divergence at one tree node.
type CompareEvent struct {
	Type   string               `json:"type,omitempty"`
	Inputs [2]CompareEventInput `json:"inputs,omitempty"`
	Diff   string               `json:"diff,omitempty"`
}

// CompareReport is the basic per-node event tree emitted by replay verify.
type CompareReport struct {
	CompareEvent
	Context  string           `json:"context,omitempty"`
	Children []*CompareReport `json:"children,omitempty"`
}

// CompareDigest returns whether subject and replay descriptors share the same
// manifest digest. This is the fastest check and is the default for
// `replay verify`.
func CompareDigest(subject, replay ocispecs.Descriptor) bool {
	return subject.Digest != "" && subject.Digest == replay.Digest
}

// CompareArtifact walks both subject and replay stores and returns a
// CompareReport describing any divergence.
//
// The implementation is intentionally basic and content-addressed. A
// manifest-digest match short-circuits the walk and returns an empty report
// (no divergence). A mismatch at any level surfaces as an event node
// populated with inputs referring to the two sides.
//
// TODO: experiment with diffoci again once it no longer requires older
// containerd APIs and private linkname-based archive wiring.
func CompareArtifact(ctx context.Context, subject, replay *Subject) (*CompareReport, error) {
	if subject == nil || replay == nil {
		return nil, errors.New("compare: nil subject or replay")
	}
	if subject.Provider == nil || replay.Provider == nil {
		return nil, errors.New("compare: nil content provider")
	}

	// Short-circuit when both descriptors already have the same digest.
	if CompareDigest(subject.Descriptor, replay.Descriptor) {
		return &CompareReport{Context: "root"}, nil
	}

	root := &CompareReport{Context: "root"}
	if err := compareDescriptor(ctx, root, subject.Provider, subject.Descriptor, replay.Provider, replay.Descriptor); err != nil {
		return root, err
	}
	return root, nil
}

// CompareSemantic is declared for API completeness. Semantic comparison is
// not yet implemented; callers receive a typed NotImplemented error.
func CompareSemantic(ctx context.Context, subject, replay *Subject) (*CompareReport, error) {
	return nil, ErrNotImplemented("--compare=semantic")
}

// compareDescriptor does a recursive content compare between two descriptors.
// When any level diverges, an Event node is attached to parent and recursion
// stops at that level.
func compareDescriptor(ctx context.Context, parent *CompareReport, pa content.Provider, da ocispecs.Descriptor, pb content.Provider, db ocispecs.Descriptor) error {
	// Descriptor-level mismatch (digest).
	if da.Digest != db.Digest {
		descA, descB := da, db
		parent.Children = append(parent.Children, &CompareReport{
			Context: fmt.Sprintf("descriptor %s", displayMediaType(da.MediaType)),
			CompareEvent: CompareEvent{
				Type: EventTypeDescriptorMismatch,
				Inputs: [2]CompareEventInput{
					{Descriptor: &descA},
					{Descriptor: &descB},
				},
				Diff: fmt.Sprintf("digest mismatch: %s vs %s", da.Digest, db.Digest),
			},
		})
		return nil
	}

	// Same digest at this level — walk descendants when available.
	switch da.MediaType {
	case ocispecs.MediaTypeImageIndex, images.MediaTypeDockerSchema2ManifestList:
		return compareIndex(ctx, parent, pa, da, pb, db)
	case ocispecs.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
		return compareManifest(ctx, parent, pa, da, pb, db)
	}
	return nil
}

func compareIndex(ctx context.Context, parent *CompareReport, pa content.Provider, da ocispecs.Descriptor, pb content.Provider, db ocispecs.Descriptor) error {
	ia, err := readIndex(ctx, pa, da)
	if err != nil {
		return err
	}
	ib, err := readIndex(ctx, pb, db)
	if err != nil {
		return err
	}
	if len(ia.Manifests) != len(ib.Manifests) {
		parent.Children = append(parent.Children, &CompareReport{
			Context: "index",
			CompareEvent: CompareEvent{
				Type: EventTypeIndexBlobMismatch,
				Inputs: [2]CompareEventInput{
					{Index: ia},
					{Index: ib},
				},
				Diff: fmt.Sprintf("child count mismatch: %d vs %d", len(ia.Manifests), len(ib.Manifests)),
			},
		})
		return nil
	}
	for i := range ia.Manifests {
		node := &CompareReport{Context: fmt.Sprintf("index/manifests[%d]", i)}
		if err := compareDescriptor(ctx, node, pa, ia.Manifests[i], pb, ib.Manifests[i]); err != nil {
			return err
		}
		if len(node.Children) > 0 || node.Type != EventTypeNone {
			parent.Children = append(parent.Children, node)
		}
	}
	return nil
}

func compareManifest(ctx context.Context, parent *CompareReport, pa content.Provider, da ocispecs.Descriptor, pb content.Provider, db ocispecs.Descriptor) error {
	ma, err := readManifest(ctx, pa, da)
	if err != nil {
		return err
	}
	mb, err := readManifest(ctx, pb, db)
	if err != nil {
		return err
	}
	if ma.Config.Digest != mb.Config.Digest {
		parent.Children = append(parent.Children, &CompareReport{
			Context: "manifest/config",
			CompareEvent: CompareEvent{
				Type: EventTypeConfigBlobMismatch,
				Inputs: [2]CompareEventInput{
					{Manifest: ma},
					{Manifest: mb},
				},
				Diff: fmt.Sprintf("config digest mismatch: %s vs %s", ma.Config.Digest, mb.Config.Digest),
			},
		})
	}
	if len(ma.Layers) != len(mb.Layers) {
		parent.Children = append(parent.Children, &CompareReport{
			Context: "manifest/layers",
			CompareEvent: CompareEvent{
				Type: EventTypeLayerBlobMismatch,
				Inputs: [2]CompareEventInput{
					{Manifest: ma},
					{Manifest: mb},
				},
				Diff: fmt.Sprintf("layer count mismatch: %d vs %d", len(ma.Layers), len(mb.Layers)),
			},
		})
		return nil
	}
	for i := range ma.Layers {
		if ma.Layers[i].Digest != mb.Layers[i].Digest {
			la, lb := ma.Layers[i], mb.Layers[i]
			parent.Children = append(parent.Children, &CompareReport{
				Context: fmt.Sprintf("manifest/layers[%d]", i),
				CompareEvent: CompareEvent{
					Type: EventTypeLayerBlobMismatch,
					Inputs: [2]CompareEventInput{
						{Descriptor: &la},
						{Descriptor: &lb},
					},
					Diff: fmt.Sprintf("layer[%d] digest mismatch: %s vs %s", i, la.Digest, lb.Digest),
				},
			})
		}
	}
	return nil
}

func readIndex(ctx context.Context, p content.Provider, desc ocispecs.Descriptor) (*ocispecs.Index, error) {
	dt, err := content.ReadBlob(ctx, p, desc)
	if err != nil {
		return nil, errors.Wrapf(err, "read index %s", desc.Digest)
	}
	var idx ocispecs.Index
	if err := json.Unmarshal(dt, &idx); err != nil {
		return nil, errors.Wrapf(err, "unmarshal index %s", desc.Digest)
	}
	return &idx, nil
}

func readManifest(ctx context.Context, p content.Provider, desc ocispecs.Descriptor) (*ocispecs.Manifest, error) {
	dt, err := content.ReadBlob(ctx, p, desc)
	if err != nil {
		return nil, errors.Wrapf(err, "read manifest %s", desc.Digest)
	}
	var mfst ocispecs.Manifest
	if err := json.Unmarshal(dt, &mfst); err != nil {
		return nil, errors.Wrapf(err, "unmarshal manifest %s", desc.Digest)
	}
	return &mfst, nil
}

func displayMediaType(mt string) string {
	if mt == "" {
		return "<unknown>"
	}
	return mt
}

// ReportMatched reports whether a CompareReport represents a successful
// compare (no divergence events). An empty tree counts as matched.
func ReportMatched(r *CompareReport) bool {
	if r == nil {
		return true
	}
	if r.Type != "" && r.Type != EventTypeNone {
		return false
	}
	for _, c := range r.Children {
		if !ReportMatched(c) {
			return false
		}
	}
	return true
}

// ReportJSON serializes a CompareReport to JSON.
// A nil report is emitted as an empty object.
func ReportJSON(r *CompareReport) ([]byte, error) {
	if r == nil {
		return []byte("{}"), nil
	}
	return json.MarshalIndent(r, "", "  ")
}
