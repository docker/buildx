package builderutil

import (
	"context"
	"os"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// Builder represents an active builder object
type Builder struct {
	dockerCli command.Cli
	txn       *store.Txn

	NodeGroup *store.NodeGroup
	Drivers   []Driver
	Err       error
}

// New initializes a new builder client
func New(dockerCli command.Cli, txn *store.Txn, name string) (_ *Builder, err error) {
	b := &Builder{
		dockerCli: dockerCli,
		txn:       txn,
	}

	if name != "" {
		b.NodeGroup, err = storeutil.GetNodeGroup(txn, dockerCli, name)
		if err != nil {
			return nil, err
		}
		return b, nil
	}

	b.NodeGroup, err = storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Validate validates builder context
func (b *Builder) Validate() error {
	if b.NodeGroup.Name == "default" && b.NodeGroup.Name != b.dockerCli.CurrentContext() {
		return errors.Errorf("use `docker --context=default buildx` to switch to default context")
	}
	list, err := b.dockerCli.ContextStore().List()
	if err != nil {
		return err
	}
	for _, l := range list {
		if l.Name == b.NodeGroup.Name && b.NodeGroup.Name != "default" {
			return errors.Errorf("use `docker --context=%s buildx` to switch to context %q", b.NodeGroup.Name, b.NodeGroup.Name)
		}
	}
	return nil
}

// GetImageOpt returns registry auth configuration
func (b *Builder) GetImageOpt() (imagetools.Opt, error) {
	return storeutil.GetImageConfig(b.dockerCli, b.NodeGroup)
}

// Boot bootstrap a builder
func (b *Builder) Boot(ctx context.Context) (bool, error) {
	toBoot := make([]int, 0, len(b.Drivers))
	for idx, d := range b.Drivers {
		if d.Err != nil || d.Driver == nil || d.Info == nil {
			continue
		}
		if d.Info.Status != driver.Running {
			toBoot = append(toBoot, idx)
		}
	}
	if len(toBoot) == 0 {
		return false, nil
	}

	printer := progress.NewPrinter(context.Background(), os.Stderr, progress.PrinterModeAuto)

	baseCtx := ctx
	eg, _ := errgroup.WithContext(ctx)
	for _, idx := range toBoot {
		func(idx int) {
			eg.Go(func() error {
				pw := progress.WithPrefix(printer, b.NodeGroup.Nodes[idx].Name, len(toBoot) > 1)
				_, err := driver.Boot(ctx, baseCtx, b.Drivers[idx].Driver, pw)
				if err != nil {
					b.Drivers[idx].Err = err
				}
				return nil
			})
		}(idx)
	}

	err := eg.Wait()
	err1 := printer.Wait()
	if err == nil {
		err = err1
	}

	return true, err
}

// GetBuilders returns all builders
func GetBuilders(dockerCli command.Cli, txn *store.Txn) ([]*Builder, error) {
	storeng, err := txn.List()
	if err != nil {
		return nil, err
	}

	currentName := "default"
	current, err := storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return nil, err
	}
	if current != nil {
		currentName = current.Name
		if current.Name == "default" {
			currentName = current.Nodes[0].Endpoint
		}
	}

	currentSet := false
	storeBuilders := make([]*Builder, len(storeng))
	for i, ng := range storeng {
		if !currentSet && ng.Name == currentName {
			ng.Current = true
			currentSet = true
		}
		storeBuilders[i] = &Builder{
			dockerCli: dockerCli,
			txn:       txn,
			NodeGroup: ng,
		}
	}

	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return nil, err
	}
	ctxBuilders := make([]*Builder, len(list))
	for i, l := range list {
		defaultNg := false
		if !currentSet && l.Name == currentName {
			defaultNg = true
			currentSet = true
		}
		ctxBuilders[i] = &Builder{
			dockerCli: dockerCli,
			txn:       txn,
			NodeGroup: &store.NodeGroup{
				Name:    l.Name,
				Current: defaultNg,
				Nodes: []store.Node{
					{
						Name:     l.Name,
						Endpoint: l.Name,
					},
				},
			},
		}
	}

	return append(storeBuilders, ctxBuilders...), nil
}
