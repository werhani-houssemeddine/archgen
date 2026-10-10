package gen

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// PruneImports removes imports whose package name is never referenced, so templates can
// list every import they might need. Blank (_) and dot imports are always kept.
// The package name is the alias, or the last path element (ignoring /vN suffixes).
func PruneImports(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	changed := false
	for i := 0; i < len(f.Decls); i++ {
		gd, ok := f.Decls[i].(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var keep []ast.Spec
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			path, _ := strconv.Unquote(is.Path.Value)
			name := importName(path)
			if is.Name != nil {
				name = is.Name.Name
			}
			if name == "_" || name == "." || used[name] {
				keep = append(keep, s)
			} else {
				changed = true
			}
		}
		gd.Specs = keep
		if len(keep) == 0 {
			f.Decls = append(f.Decls[:i], f.Decls[i+1:]...)
			i--
		}
	}
	if !changed {
		return src, nil
	}
	// rebuild f.Imports for the printer
	f.Imports = nil
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, s := range gd.Specs {
				f.Imports = append(f.Imports, s.(*ast.ImportSpec))
			}
		}
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func importName(path string) string {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && isDigits(last[1:]) {
		last = parts[len(parts)-2]
	}
	return strings.ReplaceAll(last, "-", "")
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// GroupImports rewrites the import declarations of src into three groups:
// standard library, third-party, and packages under localPrefix.
func GroupImports(src []byte, localPrefix string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if len(f.Imports) < 2 {
		return src, nil
	}
	var std, ext, local []string
	for _, is := range f.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		line := is.Path.Value
		if is.Name != nil {
			line = is.Name.Name + " " + line
		}
		switch {
		case localPrefix != "" && (path == localPrefix || strings.HasPrefix(path, localPrefix+"/")):
			local = append(local, line)
		case !strings.Contains(strings.Split(path, "/")[0], "."):
			std = append(std, line)
		default:
			ext = append(ext, line)
		}
	}
	var groups []string
	for _, g := range [][]string{std, ext, local} {
		if len(g) > 0 {
			groups = append(groups, "\t"+strings.Join(g, "\n\t"))
		}
	}
	block := "import (\n" + strings.Join(groups, "\n\n") + "\n)"
	// replace from the first import keyword to the end of the last import decl
	var first, last token.Pos
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if first == 0 {
			first = gd.Pos()
		}
		last = gd.End()
	}
	start, end := fset.Position(first).Offset, fset.Position(last).Offset
	out := string(src[:start]) + block + string(src[end:])
	return format.Source([]byte(out))
}
