package syncutil

import "sync"

type OnceValue[T any] struct {
	once  sync.Once
	value T
	err   error
}

func (o *OnceValue[T1]) Do(fn func() (T1, error)) (T1, error) {
	o.once.Do(func() {
		o.value, o.err = fn()
	})
	return o.value, o.err
}
