package localstate

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/docker/docker/pkg/ioutils"
	"github.com/pkg/errors"
)

const refsDir = "refs"

type State struct {
	LocalPath      string
	DockerfilePath string
}

type LocalState struct {
	root string
}

func New(root string) (*LocalState, error) {
	if root == "" {
		return nil, errors.Errorf("root dir empty")
	}
	if err := os.MkdirAll(filepath.Join(root, refsDir), 0700); err != nil {
		return nil, err
	}
	return &LocalState{
		root: root,
	}, nil
}

func (ls *LocalState) ReadRef(builderName, nodeName, id string) (*State, error) {
	if err := ls.validate(builderName, nodeName, id); err != nil {
		return nil, err
	}
	dt, err := os.ReadFile(filepath.Join(ls.root, refsDir, builderName, nodeName, id))
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(dt, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (ls *LocalState) SaveRef(builderName, nodeName, id string, st State) error {
	if err := ls.validate(builderName, nodeName, id); err != nil {
		return err
	}
	refDir := filepath.Join(ls.root, refsDir, builderName, nodeName)
	if err := os.MkdirAll(refDir, 0700); err != nil {
		return err
	}
	dt, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(refDir, id), dt, 0600)
}

func (ls *LocalState) RemoveBuilder(builderName string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}
	return os.RemoveAll(filepath.Join(ls.root, refsDir, builderName))
}

func (ls *LocalState) RemoveBuilderNode(builderName string, nodeName string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}
	if nodeName == "" {
		return errors.Errorf("node name empty")
	}
	return os.RemoveAll(filepath.Join(ls.root, refsDir, builderName, nodeName))
}

func (ls *LocalState) validate(builderName, nodeName, id string) error {
	if builderName == "" {
		return errors.Errorf("builder name empty")
	}
	if nodeName == "" {
		return errors.Errorf("node name empty")
	}
	if id == "" {
		return errors.Errorf("ref ID empty")
	}
	return nil
}
