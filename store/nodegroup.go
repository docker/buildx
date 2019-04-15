package store

import (
	"fmt"

	"github.com/pkg/errors"
)

type NodeGroup struct {
	Name   string
	Driver string
	Nodes  []Node
}

type Node struct {
	Name      string
	Endpoint  string
	Platforms []string
}

func (ng *NodeGroup) Leave(name string) error {
	i := ng.findNode(name)
	if i == -1 {
		return errors.Errorf("node %q not found for %s", name, ng.Name)
	}
	if len(ng.Nodes) == 1 {
		return errors.Errorf("can not leave last node, do you want to rm instance instead?")
	}
	ng.Nodes = append(ng.Nodes[:i], ng.Nodes[i+1:]...)
	return nil
}

func (ng *NodeGroup) Update(name, endpoint string, platforms []string, endpointsSet bool, actionAppend bool) error {
	i := ng.findNode(name)
	if i == -1 && !actionAppend {
		ng.Nodes = nil
	}
	if i != -1 {
		n := ng.Nodes[i]
		if endpointsSet {
			n.Endpoint = endpoint
		}
		if len(platforms) > 0 {
			n.Platforms = platforms
		}
		ng.Nodes[i] = n
		if err := ng.validateDuplicates(endpoint); err != nil {
			return err
		}
		return nil
	}

	if name == "" {
		name = ng.nextNodeName()
	}

	name, err := ValidateName(name)
	if err != nil {
		return err
	}

	n := Node{
		Name:      name,
		Endpoint:  endpoint,
		Platforms: platforms,
	}
	ng.Nodes = append(ng.Nodes, n)

	if err := ng.validateDuplicates(endpoint); err != nil {
		return err
	}
	return nil
}

func (ng *NodeGroup) validateDuplicates(ep string) error {
	// TODO: reset platforms
	i := 0
	for _, n := range ng.Nodes {
		if n.Endpoint == ep {
			i++
		}
	}
	if i > 1 {
		return errors.Errorf("invalid duplicate endpoint %s", ep)
	}
	return nil
}

func (ng *NodeGroup) findNode(name string) int {
	i := -1
	for ii, n := range ng.Nodes {
		if n.Name == name {
			i = ii
		}
	}
	return i
}

func (ng *NodeGroup) nextNodeName() string {
	i := 0
	for {
		name := fmt.Sprintf("%s%d", ng.Name, i)
		if ii := ng.findNode(name); ii != -1 {
			i++
			continue
		}
		return name
	}
}
