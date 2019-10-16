package flagutil

import "strconv"

type tristate struct {
	opt *bool
}

// Tristate is a tri-state boolean flag type.
// It can be set, but not unset.
func Tristate(opt *bool) tristate {
	return tristate{opt}
}

func (t tristate) Type() string {
	return "tristate"
}

func (t tristate) String() string {
	if t.opt == nil {
		return "(unset)"
	}
	if *t.opt {
		return "true"
	}
	return "false"
}

func (t tristate) Set(s string) error {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	t.opt = &b
	return nil
}
