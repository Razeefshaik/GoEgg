package egraph

import "strings"

// Pattern is a term with variables, used both as the left-hand side
// to search for and the right-hand side to instantiate. An Op
// beginning with "?" is a pattern variable that matches any single
// e-class; any other Op must match an e-node's Op exactly, arity and
// all, recursively matching Children against that e-node's children.
type Pattern struct {
	Op       string
	Children []Pattern
}

// PVar builds a pattern variable (matches any e-class).
func PVar(name string) Pattern { return Pattern{Op: "?" + name} }

// PNode builds a compound pattern.
func PNode(op string, children ...Pattern) Pattern { return Pattern{Op: op, Children: children} }

// IsVar reports whether p is a pattern variable.
func (p Pattern) IsVar() bool { return strings.HasPrefix(p.Op, "?") }

// Name returns the variable name (without the leading "?"). Only
// valid when IsVar() is true.
func (p Pattern) Name() string { return p.Op[1:] }

// Subst maps pattern variable names to the e-class ids they were
// bound to by a successful match.
type Subst map[string]Id

func (s Subst) clone() Subst {
	out := make(Subst, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

func (g *EGraph) matchPattern(pattern Pattern, id Id, sub Subst, out []Subst) []Subst {
	id = g.Find(id)
	if pattern.IsVar() {
		name := pattern.Name()
		if existing, ok := sub[name]; ok {
			if existing != id {
				return out
			}
			return append(out, sub.clone())
		}
		next := sub.clone()
		next[name] = id
		return append(out, next)
	}
	class := g.Class(id)
	for _, node := range class.Nodes {
		if node.Op != pattern.Op || len(node.Children) != len(pattern.Children) {
			continue
		}
		out = g.matchChildren(pattern.Children, node.Children, sub, out)
	}
	return out
}

func (g *EGraph) matchChildren(pats []Pattern, ids []Id, sub Subst, out []Subst) []Subst {
	if len(pats) == 0 {
		return append(out, sub.clone())
	}
	var head []Subst
	head = g.matchPattern(pats[0], ids[0], sub, head)
	for _, s := range head {
		out = g.matchChildren(pats[1:], ids[1:], s, out)
	}
	return out
}

// Match returns every substitution that makes pattern equal to some
// term represented by e-class id.
func (g *EGraph) Match(pattern Pattern, id Id) []Subst {
	return g.matchPattern(pattern, id, Subst{}, nil)
}

func (g *EGraph) instantiate(pattern Pattern, sub Subst) Id {
	if pattern.IsVar() {
		return sub[pattern.Name()]
	}
	children := make([]Id, len(pattern.Children))
	for i, c := range pattern.Children {
		children[i] = g.instantiate(c, sub)
	}
	return g.Add(ENode{Op: pattern.Op, Children: children})
}

// RewriteRule rewrites any term matching LHS to (an added, unioned
// copy of) RHS.
type RewriteRule struct {
	Name string
	LHS  Pattern
	RHS  Pattern
}

// Rule is a small constructor convenience.
func Rule(name string, lhs, rhs Pattern) *RewriteRule {
	return &RewriteRule{Name: name, LHS: lhs, RHS: rhs}
}

// Match pairs a rule, the e-class it matched at, and the
// substitution that made it match.
type Match struct {
	Rule  *RewriteRule
	Class Id
	Sub   Subst
}

// SearchAll finds every (rule, e-class, substitution) match for the
// given rules against every live e-class. It is read-only (it never
// mutates g) and is exactly the phase parallel_match.go parallelizes.
func (g *EGraph) SearchAll(rules []*RewriteRule) []Match {
	var matches []Match
	ids := g.ClassIds()
	for _, r := range rules {
		for _, id := range ids {
			for _, sub := range g.Match(r.LHS, id) {
				matches = append(matches, Match{Rule: r, Class: id, Sub: sub})
			}
		}
	}
	return matches
}

// ApplyAll instantiates the RHS of every match and unions it with the
// match's e-class. It does not call Rebuild; the caller must, before
// trusting the e-graph again. Returns how many unions actually
// changed something.
func (g *EGraph) ApplyAll(matches []Match) int {
	changed := 0
	for _, m := range matches {
		rhs := g.instantiate(m.Rule.RHS, m.Sub)
		if g.Union(m.Class, rhs) {
			changed++
		}
	}
	return changed
}

// Saturate repeatedly searches and applies rules (search-all, then
// apply-all, then rebuild) until either no rule matches anywhere
// (true saturation) or maxIters rounds have run. Returns the number
// of rounds actually run.
func (g *EGraph) Saturate(rules []*RewriteRule, maxIters int) int {
	for i := 0; i < maxIters; i++ {
		matches := g.SearchAll(rules)
		if len(matches) == 0 {
			return i
		}
		changed := g.ApplyAll(matches)
		g.Rebuild()
		if changed == 0 {
			return i + 1
		}
	}
	return maxIters
}
