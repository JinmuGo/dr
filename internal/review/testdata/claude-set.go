package set

// Set is a set of strings backed by a map.
type Set struct {
	items map[string]struct{}
}

// New returns an empty Set.
func New() *Set {
	return &Set{items: make(map[string]struct{})}
}

// Add adds s to the set.
func (st *Set) Add(s string) {
	st.items[s] = struct{}{}
}

// Has reports whether s is in the set.
func (st *Set) Has(s string) bool {
	_, ok := st.items[s]
	return ok
}

// Len returns the number of strings in the set.
func (st *Set) Len() int {
	return len(st.items)
}
