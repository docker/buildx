package imagetools

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/containerd/containerd/platforms"
	"github.com/docker/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func PrintManifestList(dt []byte, desc ocispec.Descriptor, refstr string, out io.Writer) error {
	ref, err := parseRef(refstr)
	if err != nil {
		return err
	}

	var mfst ocispec.Index
	if err := json.Unmarshal(dt, &mfst); err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)

	fmt.Fprintf(w, "Name:\t%s\n", ref.String())
	fmt.Fprintf(w, "MediaType:\t%s\n", desc.MediaType)
	fmt.Fprintf(w, "Digest:\t%s\n", desc.Digest)
	fmt.Fprintf(w, "\t\n")

	fmt.Fprintf(w, "Manifests:\t\n")
	w.Flush()

	pfx := "  "

	w = tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	for i, m := range mfst.Manifests {
		if i != 0 {
			fmt.Fprintf(w, "\t\n")
		}
		cr, err := reference.WithDigest(ref, m.Digest)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%sName:\t%s\n", pfx, cr.String())
		fmt.Fprintf(w, "%sMediaType:\t%s\n", pfx, m.MediaType)
		if p := m.Platform; p != nil {
			fmt.Fprintf(w, "%sPlatform:\t%s\n", pfx, platforms.Format(*p))
			if p.OSVersion != "" {
				fmt.Fprintf(w, "%sOSVersion:\t%s\n", pfx, p.OSVersion)
			}
			if len(p.OSFeatures) > 0 {
				fmt.Fprintf(w, "%sOSFeatures:\t%s\n", pfx, strings.Join(p.OSFeatures, ", "))
			}
			if len(m.URLs) > 0 {
				fmt.Fprintf(w, "%sURLs:\t%s\n", pfx, strings.Join(m.URLs, ", "))
			}
			if len(m.Annotations) > 0 {
				w.Flush()
				w2 := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
				pfx2 := pfx + "  "
				for k, v := range m.Annotations {
					fmt.Fprintf(w2, "%s%s:\t%s\n", pfx2, k, v)
				}
				w2.Flush()
			}
		}
	}

	return w.Flush()
}
