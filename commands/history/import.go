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
	file []string
}

func runImport(ctx context.Context, dockerCli command.Cli, opts importOptions) error {
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

	var urls []string

	if len(opts.file) == 0 {
		u, err := importFrom(ctx, client, os.Stdin)
		if err != nil {
			return err
		}
		urls = append(urls, u...)
	} else {
		for _, fn := range opts.file {
			var f *os.File
			var rdr io.Reader = os.Stdin
			if fn != "-" {
				f, err = os.Open(fn)
				if err != nil {
					return errors.Wrapf(err, "failed to open file %s", fn)
				}
				rdr = f
			}
			u, err := importFrom(ctx, client, rdr)
			if err != nil {
				return err
			}
			urls = append(urls, u...)
			if f != nil {
				f.Close()
			}
		}
	}

	if len(urls) == 0 {
		return errors.New("no build records found in the bundle")
	}

	for i, url := range urls {
		fmt.Fprintln(dockerCli.Err(), url)
		if i == 0 {
			err = browser.OpenURL(url)
		}
	}
	return err
}

func importFrom(ctx context.Context, c *http.Client, rdr io.Reader) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker-desktop/upload", rdr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send request, check if Docker Desktop is running")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Errorf("failed to import build: %s", string(body))
	}

	var refs []string
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&refs); err != nil {
		return nil, errors.Wrap(err, "failed to decode response")
	}

	var urls []string
	for _, ref := range refs {
		urls = append(urls, desktop.BuildURL(fmt.Sprintf(".imported/_/%s", ref)))
	}
	return urls, err
}

func importCmd(dockerCli command.Cli, _ RootOptions) *cobra.Command {
	var options importOptions

	cmd := &cobra.Command{
		Use:   "import [OPTIONS] -",
		Short: "Import build records into Docker Desktop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringArrayVarP(&options.file, "file", "f", nil, "Import from a file path")

	return cmd
}
