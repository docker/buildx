package commands

import (
	"context"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/docker/buildx/monitor/types"
)

type ListCmd struct {
	m types.Monitor

	stdout io.WriteCloser
}

func NewListCmd(m types.Monitor, stdout io.WriteCloser) types.Command {
	return &ListCmd{m, stdout}
}

func (cm *ListCmd) Info() types.CommandInfo {
	return types.CommandInfo{HelpMessage: "list buildx sessions"}
}

func (cm *ListCmd) Exec(ctx context.Context, args []string) error {
	refs, err := cm.m.List(ctx)
	if err != nil {
		return err
	}
	sort.Strings(refs)
	tw := tabwriter.NewWriter(cm.stdout, 1, 8, 1, '\t', 0)
	fmt.Fprintln(tw, "ID\tCURRENT_SESSION")
	for _, k := range refs {
		fmt.Fprintf(tw, "%-20s\t%v\n", k, k == cm.m.AttachedSessionID())
	}
	tw.Flush()
	return nil
}
