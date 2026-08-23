package egraph

import "strings"

// Term is a plain (non-e-graph) expression tree: the concrete input
// you build to add to an e-graph, and the shape of what Extract hands
// back.
type Term struct {
	Op       string
	Children []*Term
}

// Leaf builds a childless term (a variable or constant).
func Leaf(op string) *Term { return &Term{Op: op} }

// Node builds a term with children.
func Node(op string, children ...*Term) *Term { return &Term{Op: op, Children: children} }

func (t *Term) String() string {
	if t == nil {
		return "<nil>"
	}
	if len(t.Children) == 0 {
		return t.Op
	}
	parts := make([]string, len(t.Children))
	for i, c := range t.Children {
		parts[i] = c.String()
	}
	return "(" + t.Op + " " + strings.Join(parts, " ") + ")"
}

// AddTerm recursively adds t (and its children) to the e-graph and
// returns the id of the e-class representing the whole term.
func (g *EGraph) AddTerm(t *Term) Id {
	children := make([]Id, len(t.Children))
	for i, c := range t.Children {
		children[i] = g.AddTerm(c)
	}
	return g.Add(ENode{Op: t.Op, Children: children})
}
