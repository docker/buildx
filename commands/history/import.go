package history

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	remoteutil "github.com/docker/buildx/driver/remote/util"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type importOptions struct {
	file string
}

func runImport(ctx context.Context, _ command.Cli, opts importOptions) error {
	sock, err := desktop.BuildServerAddr()
	if err != nil {
		return err
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		network, addr, ok := strings.Cut(sock, "://")
		if !ok {
			return nil, errors.Errorf("invalid endpoint address: %s", sock)
		}
		return remoteutil.DialContext(ctx, network, addr)
	}

	client := &http.Client{
		Transport: tr,
	}

	var rdr io.Reader = os.Stdin
	if opts.file != "" {
		f, err := os.Open(opts.file)
		if err != nil {
			return errors.Wrap(err, "failed to open file")
		}
		defer f.Close()
		rdr = f
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker-desktop/upload", rdr)
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to send request, check if Docker Desktop is running")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Errorf("failed to import build: %s", string(body))
	}

	var refs []string
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&refs); err != nil {
		return errors.Wrap(err, "failed to decode response")
	}

	if len(refs) == 0 {
		return errors.New("no build records found in the bundle")
	}

	url := desktop.BuildURL(fmt.Sprintf(".imported/_/%s", refs[0]))
	return browser.OpenURL(url)
}

func importCmd(dockerCli command.Cli, _ RootOptions) *cobra.Command {
	var options importOptions

	cmd := &cobra.Command{
		Use:   "import [OPTIONS] < bundle.dockerbuild",
		Short: "Import a build into Docker Desktop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVarP(&options.file, "file", "f", "", "Import from a file path")

	return cmd
}
