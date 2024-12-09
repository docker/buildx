package buildflags

type comparable[E any] interface {
	Equal(other E) bool
}

func removeDupes[E comparable[E]](s []E) []E {
	// Move backwards through the slice.
	// For each element, any elements after the current element are unique.
	// If we find our current element conflicts with an existing element,
	// then we swap the offender with the end of the slice and chop it off.

	// Start at the second to last element.
	// The last element is always unique.
	for i := len(s) - 2; i >= 0; i-- {
		elem := s[i]
		// Check for duplicates after our current element.
		for j := i + 1; j < len(s); j++ {
			if elem.Equal(s[j]) {
				// Found a duplicate, exchange the
				// duplicate with the last element.
				s[j], s[len(s)-1] = s[len(s)-1], s[j]
				s = s[:len(s)-1]
				break
			}
		}
	}
	return s
}
