package util

import (
	"fmt"

	"github.com/docker/buildx/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"k8s.io/apiserver/pkg/storage/names"
)

func GenerateNodeName(builderName string, txn *store.Txn) (string, error) {
	randomName := func() (string, error) {
		u, err := uuid.NewRandom()
		if err != nil {
			return "", err
		}
		return names.SimpleNameGenerator.GenerateName(fmt.Sprintf("buildkit-%s-", u)), nil
	}

	ng, err := txn.NodeGroupByName(builderName)
	if err != nil {
		return randomName()
	}

	var name string
	for i := 0; i < 6; i++ {
		name, err = randomName()
		if err != nil {
			return "", err
		}
		exists := func(name string) bool {
			for _, n := range ng.Nodes {
				if n.Name == name {
					return true
				}
			}
			return false
		}(name)
		if exists {
			continue
		}
		return name, nil
	}

	return "", errors.Errorf("failed to generate random node name")
}
