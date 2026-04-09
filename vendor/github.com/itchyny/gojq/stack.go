package gojq

type stack struct {
	data  []block
	index int
	limit int
}

type block struct {
	value any
	next  int
}

func newStack() *stack {
	return &stack{index: -1, limit: -1}
}

func (s *stack) push(v any) {
	b := block{v, s.index}
	s.index = max(s.index, s.limit) + 1
	if s.index < len(s.data) {
		s.data[s.index] = b
	} else {
		s.data = append(s.data, b)
	}
}

func (s *stack) pop() any {
	b := s.data[s.index]
	s.index = b.next
	return b.value
}

func (s *stack) top() any {
	return s.data[s.index].value
}

func (s *stack) empty() bool {
	return s.index < 0
}

func (s *stack) save() (index, limit int) {
	index, limit = s.index, s.limit
	if s.index > s.limit {
		s.limit = s.index
	}
	return
}

func (s *stack) restore(index, limit int) {
	s.index, s.limit = index, limit
}
