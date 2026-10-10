// Package gen writes generated files safely: it never overwrites existing files unless
// forced, validates Go output with gofmt, inserts snippets at marker comments
// idempotently and adds missing imports.
package gen

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Action records what happened to a path.
type Action struct {
	Path string
	Verb string // create, overwrite, skip, update, unchanged
}

// Writer applies changes under Root.
type Writer struct {
	Root string
	// LocalPrefix (the project module path) groups project imports last.
	LocalPrefix string
	DryRun      bool
	Force       bool
	Actions     []Action
	pending     map[string][]byte // files written in this run (for dry-run reads)
}

// New returns a writer.
func New(root string, dryRun, force bool) *Writer {
	return &Writer{Root: root, DryRun: dryRun, Force: force, pending: map[string][]byte{}}
}

func (w *Writer) abs(rel string) string { return filepath.Join(w.Root, rel) }

func (w *Writer) read(rel string) ([]byte, error) {
	if b, ok := w.pending[rel]; ok {
		return b, nil
	}
	return os.ReadFile(w.abs(rel))
}

// Exists reports whether a file exists (or was created in this run).
func (w *Writer) Exists(rel string) bool {
	if _, ok := w.pending[rel]; ok {
		return true
	}
	_, err := os.Stat(w.abs(rel))
	return err == nil
}

// Write creates rel with content. Go files are gofmt'ed (a syntax error is a generator
// bug and is reported). Existing files are skipped unless Force.
func (w *Writer) Write(rel string, content []byte) error {
	if strings.HasSuffix(rel, ".go") {
		formatted, err := format.Source(content)
		if err != nil {
			return fmt.Errorf("generated %s is not valid Go: %w\n%s", rel, err, numbered(content))
		}
		if content, err = PruneImports(formatted); err != nil {
			return fmt.Errorf("generated %s: %w", rel, err)
		}
	}
	verb := "create"
	if w.Exists(rel) {
		if !w.Force {
			w.Actions = append(w.Actions, Action{rel, "skip"})
			return nil
		}
		verb = "overwrite"
	}
	return w.commit(rel, content, verb)
}

// Update replaces the content of an existing file (used for structured edits).
func (w *Writer) Update(rel string, content []byte) error {
	return w.commit(rel, content, "update")
}

func (w *Writer) commit(rel string, content []byte, verb string) error {
	w.pending[rel] = content
	w.Actions = append(w.Actions, Action{rel, verb})
	if w.DryRun {
		return nil
	}
	path := w.abs(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// Insert adds snippet on the line before the marker comment ("// archgen:<marker>")
// unless the snippet's first non-empty line is already present. Indentation of the
// marker line is applied to every snippet line.
func (w *Writer) Insert(rel, marker, snippet string) error {
	b, err := w.read(rel)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	src := string(b)
	key := firstLine(snippet)
	if key != "" && containsLine(src, key) {
		w.Actions = append(w.Actions, Action{rel, "unchanged"})
		return nil
	}
	tag := "// archgen:" + marker
	idx := strings.Index(src, tag)
	if idx < 0 {
		return fmt.Errorf("%s: marker %q not found (was it removed?); add it back or wire the code manually:\n%s", rel, tag, snippet)
	}
	lineStart := strings.LastIndex(src[:idx], "\n") + 1
	indent := src[lineStart:idx]
	var buf strings.Builder
	for _, l := range strings.Split(strings.TrimRight(snippet, "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			buf.WriteString("\n")
		} else {
			buf.WriteString(indent + l + "\n")
		}
	}
	out := src[:lineStart] + buf.String() + src[lineStart:]
	if strings.HasSuffix(rel, ".go") {
		formatted, err := format.Source([]byte(out))
		if err != nil {
			return fmt.Errorf("%s: inserting at %s produced invalid Go: %w", rel, tag, err)
		}
		out = string(formatted)
	}
	w.pending[rel] = []byte(out)
	return w.commit(rel, []byte(out), "update")
}

// EnsureImport adds an import (with optional alias) to a Go file if missing.
func (w *Writer) EnsureImport(rel, path, alias string) error {
	b, err := w.read(rel)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, b, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == path {
			return nil
		}
	}
	spec := strconv.Quote(path)
	if alias != "" {
		spec = alias + " " + spec
	}
	src := string(b)
	var out string
	switch {
	case len(f.Imports) == 0:
		// insert after package clause
		end := fset.Position(f.Name.End()).Offset
		out = src[:end] + "\n\nimport " + spec + "\n" + src[end:]
	default:
		last := f.Imports[len(f.Imports)-1]
		off := fset.Position(last.End()).Offset
		if hasParenImport(src) {
			out = src[:off] + "\n\t" + spec + src[off:]
		} else {
			// single import without parentheses: rewrite into a block
			start := fset.Position(f.Imports[0].Pos()).Offset
			kw := strings.LastIndex(src[:start], "import")
			out = src[:kw] + "import (\n\t" + src[start:off] + "\n\t" + spec + "\n)" + src[off:]
		}
	}
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return fmt.Errorf("%s: adding import %s produced invalid Go: %w", rel, path, err)
	}
	if grouped, err := GroupImports(formatted, w.LocalPrefix); err == nil {
		formatted = grouped
	}
	return w.commit(rel, formatted, "update")
}

func hasParenImport(src string) bool { return strings.Contains(src, "import (") }

// containsLine reports whether src has a line equal to line (ignoring indentation).
func containsLine(src, line string) bool {
	for _, l := range strings.Split(src, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

func numbered(b []byte) string {
	var buf bytes.Buffer
	for i, l := range strings.Split(string(b), "\n") {
		fmt.Fprintf(&buf, "%4d  %s\n", i+1, l)
	}
	return buf.String()
}

// Summary returns a human-readable report of actions, de-duplicated per path.
func (w *Writer) Summary() string {
	last := map[string]string{}
	order := []string{}
	for _, a := range w.Actions {
		if _, ok := last[a.Path]; !ok {
			order = append(order, a.Path)
		}
		// keep the most significant verb
		if prev := last[a.Path]; prev == "create" && a.Verb == "update" {
			continue
		}
		last[a.Path] = a.Verb
	}
	sort.SliceStable(order, func(i, j int) bool { return rank(last[order[i]]) < rank(last[order[j]]) })
	var b strings.Builder
	for _, p := range order {
		fmt.Fprintf(&b, "  %-9s %s\n", last[p], filepath.ToSlash(p))
	}
	return b.String()
}

func rank(v string) int {
	switch v {
	case "create":
		return 0
	case "overwrite":
		return 1
	case "update":
		return 2
	case "unchanged":
		return 3
	default:
		return 4
	}
}
