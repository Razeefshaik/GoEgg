package egraph

// Id identifies an e-class. Ids are never reused; Find(id) gives the
// current canonical representative of id's equivalence class.
type Id int64

// UnionFind is a plain sequential disjoint-set structure with path
// compression and union by rank. This is the correctness baseline that
// ConcurrentUnionFind (unionfind_concurrent.go) is checked against.
type UnionFind struct {
	parent []Id
	rank   []uint8
}

// NewUnionFind creates a union-find with no elements yet.
func NewUnionFind() *UnionFind {
	return &UnionFind{}
}

// MakeSet allocates a new singleton set and returns its id.
func (u *UnionFind) MakeSet() Id {
	id := Id(len(u.parent))
	u.parent = append(u.parent, id)
	u.rank = append(u.rank, 0)
	return id
}

// Find returns the canonical representative of id's set, compressing the
// path from id to the root as it goes.
func (u *UnionFind) Find(id Id) Id {
	root := id
	for u.parent[root] != root {
		root = u.parent[root]
	}
	// Path compression.
	for u.parent[id] != root {
		next := u.parent[id]
		u.parent[id] = root
		id = next
	}
	return root
}

// Union merges the sets containing a and b and returns the new
// representative id. If a and b are already in the same set, that set's
// representative is returned and nothing changes.
func (u *UnionFind) Union(a, b Id) Id {
	ra, rb := u.Find(a), u.Find(b)
	if ra == rb {
		return ra
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
	return ra
}

// Len reports how many elements (not sets) have been created.
func (u *UnionFind) Len() int {
	return len(u.parent)
}
