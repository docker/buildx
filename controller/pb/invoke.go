package pb

import (
	"fmt"
	"strings"
)

type CallFunc struct {
	Name         string
	Format       string
	IgnoreStatus bool
}

func (x *CallFunc) String() string {
	var elems []string
	if x.Name != "" {
		elems = append(elems, fmt.Sprintf("Name:%q", x.Name))
	}
	if x.Format != "" {
		elems = append(elems, fmt.Sprintf("Format:%q", x.Format))
	}
	if x.IgnoreStatus {
		elems = append(elems, fmt.Sprintf("IgnoreStatus:%v", x.IgnoreStatus))
	}
	return strings.Join(elems, " ")
}

type InvokeConfig struct {
	Entrypoint []string
	Cmd        []string
	NoCmd      bool
	Env        []string
	User       string
	NoUser     bool
	Cwd        string
	NoCwd      bool
	Tty        bool
	Rollback   bool
	Initial    bool
}
