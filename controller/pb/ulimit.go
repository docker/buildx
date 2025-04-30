package pb

type UlimitOpt struct {
	Values map[string]*Ulimit
}

type Ulimit struct {
	Name string
	Hard int64
	Soft int64
}
