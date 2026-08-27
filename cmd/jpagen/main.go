// jpagen generates a typed implementation of a Spring-Data-style repository
// interface. Usage (in the package that declares the interface):
//
//	//go:generate go run github.com/shubhesh07/gojpa/cmd/jpagen -type OrderRepository
//
// The interface must embed jpa.CRUD[Entity, ID]. Every other method is either
// derived from its name (FindByCustomerIdAndStatusIdIn...) or declared with
// doc-comment directives:
//
//	// jpa:query SELECT ... WHERE code IN (:codes)
//	// jpa:modifying            (for UPDATE/INSERT/DELETE; returns rows affected)
//
// Methods you implement yourself on *<Name>Impl in another file are skipped.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shubhesh07/gojpa/entity"
	"github.com/shubhesh07/gojpa/parser"
	"golang.org/x/tools/go/packages"
)

const jpaPath = "github.com/shubhesh07/gojpa/jpa"
const sqlgenPath = "github.com/shubhesh07/gojpa/sqlgen"

func main() {
	typeName := flag.String("type", "", "repository interface name (required)")
	dir := flag.String("dir", ".", "package directory")
	ctor := flag.String("ctor", "", "constructor name (default New<type>)")
	implName := flag.String("impl", "", "generated struct name (default <type>Impl)")
	entName := flag.String("entity", "", "entity type (default: from embedded jpa.CRUD[T, ID])")
	idName := flag.String("id", "", "id type, e.g. int64 or string (with -entity)")
	flag.Parse()
	if *typeName == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *ctor == "" {
		*ctor = "New" + *typeName
	}
	if *implName == "" {
		*implName = *typeName + "Impl"
	}
	if err := run(*dir, *typeName, *ctor, *implName, *entName, *idName); err != nil {
		fmt.Fprintln(os.Stderr, "jpagen:", err)
		os.Exit(1)
	}
}

type method struct {
	name string
	sig  *types.Signature
	doc  []string
}

func run(dir, name, ctor, impl, entName, idName string) error {
	outFile := filepath.Join(dir, entity.SnakeCase(name)+"_gen.go")
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedFiles, Dir: dir}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return err
	}
	pkg := pkgs[0]
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return fmt.Errorf("type %s not found in package %s", name, pkg.Name)
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return fmt.Errorf("%s is not an interface", name)
	}

	// Entity and ID from embedded jpa.CRUD[T, ID].
	var entT, idT types.Type
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		if named, ok := iface.EmbeddedType(i).(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == jpaPath && named.Obj().Name() == "CRUD" {
			entT, idT = named.TypeArgs().At(0), named.TypeArgs().At(1)
		}
	}
	if entName != "" {
		eo := pkg.Types.Scope().Lookup(entName)
		if eo == nil {
			return fmt.Errorf("entity type %s not found", entName)
		}
		entT = eo.Type()
		idT = basicType(idName)
		if idT == nil {
			return fmt.Errorf("-id must be a basic type (int64, string, ...), got %q", idName)
		}
	}
	if entT == nil {
		return fmt.Errorf("%s must embed jpa.CRUD[Entity, ID] or pass -entity/-id", name)
	}
	resolve, err := resolver(entT)
	if err != nil {
		return err
	}

	// Doc comments and user-implemented methods come from the AST.
	docs := map[string][]string{}
	implemented := map[string]bool{}
	for i, f := range pkg.Syntax {
		if filepath.Base(pkg.GoFiles[i]) == filepath.Base(outFile) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.TypeSpec:
				if it, ok := d.Type.(*ast.InterfaceType); ok && d.Name.Name == name {
					for _, m := range it.Methods.List {
						if len(m.Names) == 1 && m.Doc != nil {
							for _, c := range m.Doc.List {
								docs[m.Names[0].Name] = append(docs[m.Names[0].Name], strings.TrimSpace(strings.TrimPrefix(c.Text, "//")))
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil && len(d.Recv.List) == 1 && recvName(d.Recv.List[0].Type) == impl {
					implemented[d.Name.Name] = true
				}
			}
			return true
		})
	}

	var methods []method
	for i := 0; i < iface.NumExplicitMethods(); i++ {
		m := iface.ExplicitMethod(i)
		if implemented[m.Name()] {
			continue
		}
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			return fmt.Errorf("%s: unexpected method type", m.Name())
		}
		methods = append(methods, method{name: m.Name(), sig: sig, doc: docs[m.Name()]})
	}

	g := &gen{pkg: pkg.Types, imports: map[string]string{}, entT: entT, idT: idT, ifaceName: name}
	g.imports[jpaPath] = "jpa"
	g.imports[sqlgenPath] = "sqlgen"
	g.imports["context"] = "context"
	var body bytes.Buffer
	fmt.Fprintf(&body, "// %s implements %s. Add hand-written methods on *%s in another file.\n", impl, name, impl)
	fmt.Fprintf(&body, "type %s struct {\n\t*jpa.Repository[%s, %s]\n}\n\n", impl, g.typ(entT), g.typ(idT))
	fmt.Fprintf(&body, "var _ %s = (*%s)(nil)\n\n", name, impl)
	fmt.Fprintf(&body, "// %s builds the repository on exec (*sql.DB or *sql.Tx).\nfunc %s(exec jpa.Executor, d sqlgen.Dialect, opts ...jpa.Option) *%s {\n\treturn &%s{Repository: jpa.New[%s, %s](exec, d, append([]jpa.Option{jpa.WithName(%q)}, opts...)...)}\n}\n\n",
		ctor, ctor, impl, impl, g.typ(entT), g.typ(idT), pkg.Name+"."+name)
	fmt.Fprintf(&body, "// WithTx returns a copy bound to tx.\nfunc (r *%s) WithTx(exec jpa.Executor) *%s {\n\treturn &%s{Repository: r.Repository.WithTx(exec)}\n}\n\n", impl, impl, impl)

	for _, m := range methods {
		code, err := g.method(impl, m, resolve)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", name, m.name, err)
		}
		body.WriteString(code)
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by jpagen; DO NOT EDIT.\n\npackage %s\n\nimport (\n", pkg.Name)
	paths := make([]string, 0, len(g.imports))
	for p := range g.imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if filepath.Base(p) == g.imports[p] {
			fmt.Fprintf(&out, "\t%q\n", p)
		} else {
			fmt.Fprintf(&out, "\t%s %q\n", g.imports[p], p)
		}
	}
	out.WriteString(")\n\n")
	out.Write(body.Bytes())
	src, err := format.Source(out.Bytes())
	if err != nil {
		return fmt.Errorf("format: %w\n%s", err, out.String())
	}
	return os.WriteFile(outFile, src, 0o644)
}

func recvName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// resolver builds a property resolver from the entity's struct fields, using
// the same rules as entity.Meta.ResolveProperty.
func resolver(t types.Type) (parser.Resolver, error) {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("entity %s is not a struct", t)
	}
	names := map[string]bool{}
	var walk func(*types.Struct)
	walk = func(st *types.Struct) {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			tag := reflectTag(st.Tag(i), "db")
			if tag == "-" {
				continue
			}
			if f.Embedded() {
				if es, ok := f.Type().Underlying().(*types.Struct); ok {
					walk(es)
					continue
				}
			}
			if !f.Exported() {
				continue
			}
			col := tag
			if col == "" {
				col = entity.SnakeCase(f.Name())
			}
			names[strings.ToLower(f.Name())] = true
			names[strings.ReplaceAll(col, "_", "")] = true
		}
	}
	walk(st)
	return func(p string) bool { return names[strings.ToLower(strings.ReplaceAll(p, "_", ""))] }, nil
}

func reflectTag(tag, key string) string {
	for _, part := range strings.Fields(tag) {
		if strings.HasPrefix(part, key+":") {
			v := strings.TrimPrefix(part, key+":")
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

type gen struct {
	pkg       *types.Package
	imports   map[string]string
	entT, idT types.Type
	ifaceName string
}

func (g *gen) typ(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == g.pkg {
			return ""
		}
		g.imports[p.Path()] = p.Name()
		return p.Name()
	})
}

func (g *gen) method(impl string, m method, resolve parser.Resolver) (string, error) {
	sig := m.sig
	params := sig.Params()
	if params.Len() == 0 || !isContext(params.At(0).Type()) {
		return "", fmt.Errorf("first parameter must be context.Context")
	}
	var sigParams, argNames []string
	for i := 0; i < params.Len(); i++ {
		p := params.At(i)
		n := p.Name()
		if n == "" || n == "_" {
			n = fmt.Sprintf("p%d", i)
		}
		sigParams = append(sigParams, n+" "+g.typ(p.Type()))
		if i > 0 {
			argNames = append(argNames, n)
		}
	}
	res := sig.Results()
	var results []string
	for i := 0; i < res.Len(); i++ {
		results = append(results, g.typ(res.At(i).Type()))
	}
	resStr := strings.Join(results, ", ")
	if len(results) > 1 {
		resStr = "(" + resStr + ")"
	}
	head := fmt.Sprintf("func (r *%s) %s(%s) %s {\n", impl, m.name, strings.Join(sigParams, ", "), resStr)
	ctx := strings.SplitN(sigParams[0], " ", 2)[0]
	qname := fmt.Sprintf("%q", m.name)

	body, err := g.body(m, resolve, ctx, qname, argNames, params, res)
	if err != nil {
		return "", err
	}
	return head + body + "}\n\n", nil
}

func (g *gen) body(m method, resolve parser.Resolver, ctx, qname string, argNames []string, params, res *types.Tuple) (string, error) {
	if res.Len() == 0 || res.Len() > 2 || !isError(res.At(res.Len()-1).Type()) {
		return "", fmt.Errorf("must return error or (T, error)")
	}
	// Declared query?
	var query, count string
	modifying := false
	cur := &query
	for _, d := range m.doc {
		switch {
		case strings.HasPrefix(d, "jpa:query"):
			query = strings.TrimSpace(strings.TrimPrefix(d, "jpa:query"))
			cur = &query
		case strings.HasPrefix(d, "jpa:count"):
			count = strings.TrimSpace(strings.TrimPrefix(d, "jpa:count"))
			cur = &count
		case strings.HasPrefix(d, "jpa:modifying"):
			modifying = true
		case *cur != "" && !strings.HasPrefix(d, "jpa:"):
			*cur += " " + d // continuation line
		}
	}
	if query != "" {
		var kv []string
		pageable := ""
		for i, n := range argNames {
			if isJpa(params.At(i+1).Type(), "Pageable") {
				pageable = n
				continue
			}
			kv = append(kv, fmt.Sprintf("%q: %s", params.At(i+1).Name(), n))
		}
		pm := "map[string]any{" + strings.Join(kv, ", ") + "}"
		q := fmt.Sprintf("%q", query)
		if rt := res.At(0).Type(); res.Len() == 2 && pageable != "" && (isJpa(rt, "Page") || isJpa(rt, "Slice")) {
			elem := g.typ(rt.(*types.Named).TypeArgs().At(0))
			if isJpa(rt, "Slice") {
				return fmt.Sprintf("\treturn jpa.SelectSlice[%s](%s, r, %s, %s, %s, %s)\n", elem, ctx, qname, q, pageable, pm), nil
			}
			if count == "" {
				return "", fmt.Errorf("jpa.Page return with jpa:query needs a jpa:count query")
			}
			return fmt.Sprintf("\treturn jpa.SelectPage[%s](%s, r, %s, %s, %q, %s, %s)\n", elem, ctx, qname, q, count, pageable, pm), nil
		}
		if modifying {
			if res.Len() == 1 {
				return fmt.Sprintf("\t_, err := r.Exec(%s, %s, %s, %s)\n\treturn err\n", ctx, qname, q, pm), nil
			}
			if !isInt64(res.At(0).Type()) {
				return "", fmt.Errorf("jpa:modifying must return (int64, error) or error")
			}
			return fmt.Sprintf("\treturn r.Exec(%s, %s, %s, %s)\n", ctx, qname, q, pm), nil
		}
		if res.Len() != 2 {
			return "", fmt.Errorf("jpa:query must return (rows, error)")
		}
		rt := res.At(0).Type()
		switch {
		case isSlice(rt) && isBasic(rt.(*types.Slice).Elem()):
			return fmt.Sprintf("\treturn jpa.SelectScalars[%s](%s, r, %s, %s, %s)\n", g.typ(rt.(*types.Slice).Elem()), ctx, qname, q, pm), nil
		case isSlice(rt):
			return fmt.Sprintf("\treturn jpa.Select[%s](%s, r, %s, %s, %s)\n", g.typ(rt.(*types.Slice).Elem()), ctx, qname, q, pm), nil
		case isPtr(rt):
			return fmt.Sprintf("\treturn jpa.SelectOne[%s](%s, r, %s, %s, %s)\n", g.typ(rt.(*types.Pointer).Elem()), ctx, qname, q, pm), nil
		default:
			return fmt.Sprintf("\treturn jpa.SelectScalar[%s](%s, r, %s, %s, %s)\n", g.typ(rt), ctx, qname, q, pm), nil
		}
	}

	// Derived query.
	tree, err := parser.Parse(m.name, resolve)
	if err != nil {
		return "", err
	}
	bind := argNames
	special := ""
	if n := len(bind); n > 0 {
		if t := params.At(n).Type(); isJpa(t, "Pageable") || isJpa(t, "Sort") || isJpa(t, "Limit") || isJpa(t, "ScrollPosition") {
			special, bind = bind[n-1], bind[:n-1]
		}
	}
	if len(bind) != tree.NumArgs {
		return "", fmt.Errorf("method name needs %d argument(s) after ctx, signature has %d", tree.NumArgs, len(bind))
	}
	args := strings.Join(append([]string{ctx, qname}, bind...), ", ")
	if res.Len() == 1 { // error only
		if tree.Verb != parser.Delete {
			return "", fmt.Errorf("only delete methods may return just error")
		}
		return fmt.Sprintf("\t_, err := r.DeleteBy(%s)\n\treturn err\n", args), nil
	}
	rt := res.At(0).Type()
	switch {
	case tree.Verb == parser.Count && isInt64(rt):
		return fmt.Sprintf("\treturn r.CountBy(%s)\n", args), nil
	case tree.Verb == parser.Delete && isInt64(rt):
		return fmt.Sprintf("\treturn r.DeleteBy(%s)\n", args), nil
	case tree.Verb == parser.Exists && isBool(rt):
		return fmt.Sprintf("\treturn r.ExistsBy(%s)\n", args), nil
	case tree.Verb == parser.Find && isJpa(rt, "Page"):
		if special == "" {
			return "", fmt.Errorf("jpa.Page return needs a jpa.Pageable parameter")
		}
		return fmt.Sprintf("\treturn r.PageBy(%s, %s, %s%s)\n", ctx, qname, special, joinArgs(bind)), nil
	case tree.Verb == parser.Find && isJpa(rt, "Window"):
		if special == "" {
			return "", fmt.Errorf("jpa.Window return needs a jpa.ScrollPosition parameter")
		}
		return fmt.Sprintf("\treturn r.WindowBy(%s, %s, %s%s)\n", ctx, qname, special, joinArgs(bind)), nil
	case tree.Verb == parser.Find && isJpa(rt, "Slice"):
		if special == "" {
			return "", fmt.Errorf("jpa.Slice return needs a jpa.Pageable parameter")
		}
		return fmt.Sprintf("\treturn r.SliceBy(%s, %s, %s%s)\n", ctx, qname, special, joinArgs(bind)), nil
	case tree.Verb == parser.Find && isSlice(rt) && types.Identical(rt.(*types.Slice).Elem(), g.entT):
		return fmt.Sprintf("\treturn r.FindBy(%s%s)\n", args, joinArgs(specialList(special))), nil
	case tree.Verb == parser.Find && isPtr(rt) && types.Identical(rt.(*types.Pointer).Elem(), g.entT):
		return fmt.Sprintf("\treturn r.FindOneBy(%s%s)\n", args, joinArgs(specialList(special))), nil
	}
	return "", fmt.Errorf("return type %s does not match %s query", g.typ(rt), tree.Verb)
}

func specialList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func joinArgs(a []string) string {
	if len(a) == 0 {
		return ""
	}
	return ", " + strings.Join(a, ", ")
}

func isContext(t types.Type) bool { return isNamed(t, "context", "Context") }
func isError(t types.Type) bool   { return types.Identical(t, types.Universe.Lookup("error").Type()) }
func isInt64(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Int64
}
func isBool(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}
func isSlice(t types.Type) bool         { _, ok := t.(*types.Slice); return ok }
func isPtr(t types.Type) bool           { _, ok := t.(*types.Pointer); return ok }
func isJpa(t types.Type, n string) bool { return isNamed(t, jpaPath, n) }

func isNamed(t types.Type, path, name string) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func isBasic(t types.Type) bool { _, ok := t.Underlying().(*types.Basic); return ok }

func basicType(name string) types.Type {
	if o := types.Universe.Lookup(name); o != nil {
		if _, ok := o.Type().Underlying().(*types.Basic); ok {
			return o.Type()
		}
	}
	return nil
}
