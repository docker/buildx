package cobrautil

type BoolFuncValue func(string) error

func (f BoolFuncValue) Set(s string) error { return f(s) }

func (f BoolFuncValue) String() string { return "" }

func (f BoolFuncValue) Type() string { return "bool" }

func (f BoolFuncValue) IsBoolFlag() bool { return true }
