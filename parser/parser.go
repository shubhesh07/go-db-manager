// Package parser is a port of Spring Data's PartTree: it turns a repository
// method name such as FindTop5ByCustomerIdAndStatusIdInOrderByCreatedOnDesc
// into a structured query description. It knows nothing about SQL.
package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Verb string

const (
	Find   Verb = "find"
	Count  Verb = "count"
	Exists Verb = "exists"
	Delete Verb = "delete"
)

// Keyword is a predicate operator. NumArgs is how many method arguments it
// consumes.
type Keyword struct {
	Name    string
	NumArgs int
}

// Keywords is ordered longest-first, exactly like Spring's Part.Type enum.
var Keywords = []Keyword{
	{"IsNotNull", 0}, {"NotNull", 0}, {"IsNull", 0}, {"Null", 0},
	{"IsNotIn", 1}, {"NotIn", 1}, {"IsIn", 1}, {"In", 1},
	{"IsNotLike", 1}, {"NotLike", 1}, {"IsLike", 1}, {"Like", 1},
	{"IsStartingWith", 1}, {"StartingWith", 1}, {"StartsWith", 1},
	{"IsEndingWith", 1}, {"EndingWith", 1}, {"EndsWith", 1},
	{"IsNotContaining", 1}, {"NotContaining", 1}, {"NotContains", 1},
	{"IsContaining", 1}, {"Containing", 1}, {"Contains", 1},
	{"IsBetween", 2}, {"Between", 2},
	{"IsBefore", 1}, {"Before", 1}, {"IsAfter", 1}, {"After", 1},
	{"IsLessThanEqual", 1}, {"LessThanEqual", 1}, {"IsLessThan", 1}, {"LessThan", 1},
	{"IsGreaterThanEqual", 1}, {"GreaterThanEqual", 1}, {"IsGreaterThan", 1}, {"GreaterThan", 1},
	{"IsTrue", 0}, {"True", 0}, {"IsFalse", 0}, {"False", 0},
	{"IsNot", 1}, {"Not", 1},
	{"Is", 1}, {"Equals", 1},
}

// Canonical maps every alias to the operator name used by sqlgen.
var Canonical = map[string]string{
	"IsNotNull": "IsNotNull", "NotNull": "IsNotNull", "IsNull": "IsNull", "Null": "IsNull",
	"IsNotIn": "NotIn", "NotIn": "NotIn", "IsIn": "In", "In": "In",
	"IsNotLike": "NotLike", "NotLike": "NotLike", "IsLike": "Like", "Like": "Like",
	"IsStartingWith": "StartingWith", "StartingWith": "StartingWith", "StartsWith": "StartingWith",
	"IsEndingWith": "EndingWith", "EndingWith": "EndingWith", "EndsWith": "EndingWith",
	"IsNotContaining": "NotContaining", "NotContaining": "NotContaining", "NotContains": "NotContaining",
	"IsContaining": "Containing", "Containing": "Containing", "Contains": "Containing",
	"IsBetween": "Between", "Between": "Between",
	"IsBefore": "LessThan", "Before": "LessThan", "IsAfter": "GreaterThan", "After": "GreaterThan",
	"IsLessThanEqual": "LessThanEqual", "LessThanEqual": "LessThanEqual", "IsLessThan": "LessThan", "LessThan": "LessThan",
	"IsGreaterThanEqual": "GreaterThanEqual", "GreaterThanEqual": "GreaterThanEqual", "IsGreaterThan": "GreaterThan", "GreaterThan": "GreaterThan",
	"IsTrue": "True", "True": "True", "IsFalse": "False", "False": "False",
	"IsNot": "Not", "Not": "Not",
	"Is": "Equals", "Equals": "Equals",
}

type Part struct {
	Property   string // CamelCase as written, e.g. "CustomerId"
	Op         string // canonical operator, see Canonical
	NumArgs    int
	IgnoreCase bool
}

type Order struct {
	Property string
	Desc     bool
}

type Tree struct {
	Verb     Verb
	Distinct bool
	Limit    int      // Top/First N; 0 = none
	Or       [][]Part // OR of ANDs
	OrderBy  []Order
	NumArgs  int
}

var (
	subjectRe = regexp.MustCompile(`^(?i:(find|read|get|query|search|stream|count|exists|delete|remove))(.*?)(By(.*))?$`)
	limitRe   = regexp.MustCompile(`(First|Top)(\d*)`)
)

// Resolver reports whether a CamelCase property exists on the entity.
type Resolver func(property string) bool

// Parse parses a method name. resolve may be nil (no property validation).
func Parse(name string, resolve Resolver) (*Tree, error) {
	m := subjectRe.FindStringSubmatch(name)
	if m == nil {
		return nil, fmt.Errorf("parser: %q must start with find/read/get/query/search/stream/count/exists/delete/remove", name)
	}
	t := &Tree{}
	switch strings.ToLower(m[1]) {
	case "count":
		t.Verb = Count
	case "exists":
		t.Verb = Exists
	case "delete", "remove":
		t.Verb = Delete
	default:
		t.Verb = Find
	}
	subject := m[2]
	if strings.Contains(subject, "Distinct") {
		t.Distinct = true
	}
	if lm := limitRe.FindStringSubmatch(subject); lm != nil {
		t.Limit = 1
		if n, err := strconv.Atoi(lm[2]); err == nil && n > 0 {
			t.Limit = n
		}
	}
	if m[3] == "" { // no "By": findAll, count, deleteAll
		return t, nil
	}
	predicate := m[4]

	criteria, order := predicate, ""
	if i := strings.Index(predicate, "OrderBy"); i >= 0 {
		criteria, order = predicate[:i], predicate[i+len("OrderBy"):]
	}
	allIgnoreCase := false
	for _, s := range []string{"AllIgnoreCase", "AllIgnoringCase"} {
		if strings.HasSuffix(criteria, s) {
			allIgnoreCase = true
			criteria = strings.TrimSuffix(criteria, s)
		}
	}
	if criteria != "" {
		for _, orSeg := range splitToken(criteria, "Or") {
			var ands []Part
			for _, andSeg := range splitToken(orSeg, "And") {
				p, err := parsePart(andSeg, resolve)
				if err != nil {
					return nil, fmt.Errorf("parser: %q: %w", name, err)
				}
				p.IgnoreCase = p.IgnoreCase || allIgnoreCase
				t.NumArgs += p.NumArgs
				ands = append(ands, p)
			}
			t.Or = append(t.Or, ands)
		}
	}
	if order != "" {
		ob, err := parseOrder(order, resolve)
		if err != nil {
			return nil, fmt.Errorf("parser: %q: %w", name, err)
		}
		t.OrderBy = ob
	}
	return t, nil
}

// splitToken splits s on the CamelCase token tok ("And"/"Or"): the token must
// be followed by an upper-case letter, digit, underscore or the end, so
// "Order" and "Vendor" are never split.
func splitToken(s, tok string) []string {
	var out []string
	start := 0
	for i := 0; i+len(tok) <= len(s); i++ {
		if s[i:i+len(tok)] != tok || i == start {
			continue
		}
		next := i + len(tok)
		if next < len(s) && !isUpperDigitOrUnderscore(s[next]) {
			continue
		}
		out = append(out, s[start:i])
		start = next
		i = next - 1
	}
	return append(out, s[start:])
}

func parsePart(seg string, resolve Resolver) (Part, error) {
	p := Part{}
	for _, s := range []string{"IgnoreCase", "IgnoringCase"} {
		if strings.HasSuffix(seg, s) {
			p.IgnoreCase = true
			seg = strings.TrimSuffix(seg, s)
		}
	}
	seg = strings.TrimSuffix(seg, "_")
	if seg == "" {
		return p, fmt.Errorf("empty criteria segment")
	}
	if resolve != nil && resolve(seg) {
		// Whole segment is a property (e.g. "CheckIn"); like Spring, prefer
		// the property when it resolves as-is.
		p.Property, p.Op, p.NumArgs = seg, "Equals", 1
		return p, nil
	}
	for _, kw := range Keywords {
		if strings.HasSuffix(seg, kw.Name) && len(seg) > len(kw.Name) {
			prop := strings.TrimSuffix(strings.TrimSuffix(seg, kw.Name), "_")
			if resolve != nil && !resolve(prop) {
				continue
			}
			p.Property, p.Op, p.NumArgs = prop, Canonical[kw.Name], kw.NumArgs
			return p, nil
		}
	}
	if resolve != nil {
		return p, fmt.Errorf("unknown property %q", seg)
	}
	p.Property, p.Op, p.NumArgs = seg, "Equals", 1
	return p, nil
}

// parseOrder parses "CreatedOnDescIdAsc": a property ends at an Asc/Desc
// token; a trailing property without direction is ASC.
func parseOrder(s string, resolve Resolver) ([]Order, error) {
	var out []Order
	rest := s
	for rest != "" {
		found := false
		for i := 1; i <= len(rest) && !found; i++ {
			head := rest[:i]
			var dir string
			switch {
			case strings.HasSuffix(head, "Desc") && len(head) > 4:
				dir = "Desc"
			case strings.HasSuffix(head, "Asc") && len(head) > 3:
				dir = "Asc"
			default:
				continue
			}
			if i < len(rest) && (rest[i] < 'A' || rest[i] > 'Z') {
				continue
			}
			prop := strings.TrimSuffix(strings.TrimSuffix(head, dir), "_")
			if resolve != nil && !resolve(prop) {
				continue
			}
			out = append(out, Order{Property: prop, Desc: dir == "Desc"})
			rest = rest[i:]
			found = true
		}
		if !found {
			prop := strings.TrimSuffix(rest, "_")
			if resolve != nil && !resolve(prop) {
				return nil, fmt.Errorf("unknown order property %q", prop)
			}
			out = append(out, Order{Property: prop})
			break
		}
	}
	return out, nil
}

func isUpperDigitOrUnderscore(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
