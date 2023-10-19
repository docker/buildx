package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/docker/buildx/localstate"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/gofrs/flock"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const (
	instanceDir = "instances"
	defaultsDir = "defaults"
	activityDir = "activity"
)

func New(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, instanceDir), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, defaultsDir), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, activityDir), 0700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

type Store struct {
	root string
}

func (s *Store) Txn() (*Txn, func(), error) {
	l := flock.New(filepath.Join(s.root, ".lock"))
	if err := l.Lock(); err != nil {
		return nil, nil, err
	}
	return &Txn{
			s: s,
		}, func() {
			l.Close()
		}, nil
}

type Txn struct {
	s *Store
}

func (t *Txn) List() ([]*NodeGroup, error) {
	pp := filepath.Join(t.s.root, instanceDir)
	fis, err := os.ReadDir(pp)
	if err != nil {
		return nil, err
	}
	ngs := make([]*NodeGroup, 0, len(fis))
	for _, fi := range fis {
		ng, err := t.NodeGroupByName(fi.Name())
		if err != nil {
			if os.IsNotExist(errors.Cause(err)) {
				os.RemoveAll(filepath.Join(pp, fi.Name()))
				continue
			}
			return nil, err
		}
		ngs = append(ngs, ng)
	}

	sort.Slice(ngs, func(i, j int) bool {
		return ngs[i].Name < ngs[j].Name
	})

	return ngs, nil
}

func (t *Txn) NodeGroupByName(name string) (*NodeGroup, error) {
	name, err := ValidateName(name)
	if err != nil {
		return nil, err
	}
	dt, err := os.ReadFile(filepath.Join(t.s.root, instanceDir, name))
	if err != nil {
		return nil, err
	}
	var ng NodeGroup
	if err := json.Unmarshal(dt, &ng); err != nil {
		return nil, err
	}
	if ng.LastActivity, err = t.GetLastActivity(&ng); err != nil {
		return nil, err
	}
	return &ng, nil
}

func (t *Txn) Save(ng *NodeGroup) error {
	name, err := ValidateName(ng.Name)
	if err != nil {
		return err
	}
	if err := t.UpdateLastActivity(ng); err != nil {
		return err
	}
	dt, err := json.Marshal(ng)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(t.s.root, instanceDir, name), dt, 0600)
}

func (t *Txn) Remove(name string) error {
	name, err := ValidateName(name)
	if err != nil {
		return err
	}
	if err := t.RemoveLastActivity(name); err != nil {
		return err
	}
	ls, err := localstate.New(t.s.root)
	if err != nil {
		return err
	}
	if err := ls.RemoveBuilder(name); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(t.s.root, instanceDir, name))
}

func (t *Txn) SetCurrent(key, name string, global, def bool) error {
	c := current{
		Key:    key,
		Name:   name,
		Global: global,
	}
	dt, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := ioutils.AtomicWriteFile(filepath.Join(t.s.root, "current"), dt, 0600); err != nil {
		return err
	}

	h := toHash(key)

	if def {
		if err := ioutils.AtomicWriteFile(filepath.Join(t.s.root, defaultsDir, h), []byte(name), 0600); err != nil {
			return err
		}
	} else {
		os.RemoveAll(filepath.Join(t.s.root, defaultsDir, h)) // ignore error
	}
	return nil
}

func (t *Txn) UpdateLastActivity(ng *NodeGroup) error {
	return ioutils.AtomicWriteFile(filepath.Join(t.s.root, activityDir, ng.Name), []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
}

func (t *Txn) GetLastActivity(ng *NodeGroup) (la time.Time, _ error) {
	dt, err := os.ReadFile(filepath.Join(t.s.root, activityDir, ng.Name))
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			return la, nil
		}
		return la, err
	}
	return time.Parse(time.RFC3339, string(dt))
}

func (t *Txn) RemoveLastActivity(name string) error {
	name, err := ValidateName(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(t.s.root, activityDir, name))
}

func (t *Txn) reset(key string) error {
	dt, err := json.Marshal(current{Key: key})
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(t.s.root, "current"), dt, 0600)
}

func (t *Txn) Current(key string) (*NodeGroup, error) {
	dt, err := os.ReadFile(filepath.Join(t.s.root, "current"))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err == nil {
		var c current
		if err := json.Unmarshal(dt, &c); err != nil {
			return nil, err
		}
		if c.Name != "" {
			if c.Global {
				ng, err := t.NodeGroupByName(c.Name)
				if err == nil {
					return ng, nil
				}
			}

			if c.Key == key {
				ng, err := t.NodeGroupByName(c.Name)
				if err == nil {
					return ng, nil
				}
				return nil, nil
			}
		}
	}

	h := toHash(key)

	dt, err = os.ReadFile(filepath.Join(t.s.root, defaultsDir, h))
	if err != nil {
		if os.IsNotExist(err) {
			t.reset(key)
			return nil, nil
		}
		return nil, err
	}

	ng, err := t.NodeGroupByName(string(dt))
	if err != nil {
		t.reset(key)
	}
	if err := t.SetCurrent(key, string(dt), false, true); err != nil {
		return nil, err
	}
	return ng, nil
}

type current struct {
	Key    string
	Name   string
	Global bool
}

func toHash(in string) string {
	return digest.FromBytes([]byte(in)).Hex()[:20]
}
