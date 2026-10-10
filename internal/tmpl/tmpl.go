// Package tmpl holds the embedded code templates and the data passed to them.
package tmpl

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/werhani-houssemeddine/archgen/internal/fields"
	"github.com/werhani-houssemeddine/archgen/internal/naming"
)

//go:embed templates
var files embed.FS

var funcs = template.FuncMap{
	"pascal": naming.Pascal,
	"camel":  naming.Camel,
	"snake":  naming.Snake,
	"kebab":  naming.Kebab,
	"plural": naming.Plural,
	"human":  naming.Human,
	"lower":  strings.ToLower,
	"upper":  strings.ToUpper,
	"join":   strings.Join,
	"quote":  func(s string) string { return fmt.Sprintf("%q", s) },
	"receiver": func(s string) string {
		c := naming.Camel(s)
		if c == "" {
			return "x"
		}
		return strings.ToLower(c[:1])
	},
}

var root = template.Must(template.New("root").Funcs(funcs).ParseFS(files, "templates/*.tmpl"))

// Render executes the named template ("resource_domain.go.tmpl").
func Render(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	t := root.Lookup(name)
	if t == nil {
		return nil, fmt.Errorf("template %q not found", name)
	}
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

/* ------------------------------------------------------------------ */
/* Template data                                                       */
/* ------------------------------------------------------------------ */

// Project is project-wide data.
type Project struct {
	AppName      string // "backend" (service name, DB name, JWT issuer)
	Module       string // go module path
	Kit          string // archkit import path
	KitReplace   string // optional local path for a replace directive
	GoVersion    string
	Driver       string // pgx | pq
	DriverImport string
	DriverName   string
	APIPrefix    string
	ModulesDir   string // "internal/modules"
}

// ModuleImport returns the import path of a module package.
func (p Project) ModuleImport(pkg string, sub ...string) string {
	return strings.Join(append([]string{p.Module, p.ModulesDir, pkg}, sub...), "/")
}

// Module describes a business module.
type Module struct {
	Pkg    string // package & directory: "userprofiles"
	Pascal string // "UserProfiles"
	Snake  string // DB schema / permission prefix: "user_profiles"
	Kebab  string // "user-profiles"
	Human  string // "user profiles"
}

// NewModule derives names from user input.
func NewModule(name string) Module {
	return Module{Pkg: naming.Package(name), Pascal: naming.Pascal(name), Snake: naming.Snake(name), Kebab: naming.Kebab(name), Human: naming.Human(name)}
}

// Resource describes an aggregate and its CRUD slice.
type Resource struct {
	Name        string // "CourseSession"
	Camel       string // "courseSession"
	Snake       string // "course_session"
	Human       string // "course session"
	Plural      string // "CourseSessions"
	PluralCamel string
	PluralSnake string // "course_sessions"
	PluralKebab string // "course-sessions" (URL path)
	PluralHuman string
	Table       string // "courses.course_sessions"
	Perm        string // permission resource: "course_sessions"
	Fields      []fields.Field
}

// NewResource derives names; module provides the schema.
func NewResource(m Module, name string, fs []fields.Field) Resource {
	singular := naming.Pascal(naming.Singular(naming.Pascal(name)))
	plural := naming.Plural(singular)
	return Resource{
		Name: singular, Camel: naming.Camel(singular), Snake: naming.Snake(singular), Human: naming.Human(singular),
		Plural: plural, PluralCamel: naming.Camel(plural), PluralSnake: naming.Snake(plural), PluralKebab: naming.Kebab(plural),
		PluralHuman: naming.Human(plural), Table: m.Snake + "." + naming.Snake(plural), Perm: naming.Snake(plural), Fields: fs,
	}
}

// HasRequired reports whether any field fails validation when empty.
func (r Resource) HasRequired() bool {
	for _, f := range r.Fields {
		if f.Required() {
			return true
		}
	}
	return false
}

// Searchable returns text-searchable fields.
func (r Resource) Searchable() []fields.Field {
	var out []fields.Field
	for _, f := range r.Fields {
		if f.Searchable() {
			out = append(out, f)
		}
	}
	return out
}

// Unique returns unique fields.
func (r Resource) Unique() []fields.Field {
	var out []fields.Field
	for _, f := range r.Fields {
		if f.Unique {
			out = append(out, f)
		}
	}
	return out
}

// Indexed returns fields with an index modifier.
func (r Resource) Indexed() []fields.Field {
	var out []fields.Field
	for _, f := range r.Fields {
		if f.Index {
			out = append(out, f)
		}
	}
	return out
}

// Sortable returns fields clients may sort by (scalar, non-text).
func (r Resource) Sortable() []fields.Field {
	var out []fields.Field
	for _, f := range r.Fields {
		if f.Kind != "text" {
			out = append(out, f)
		}
	}
	return out
}

// SampleJSONBody returns a valid JSON body for HTTP tests.
func (r Resource) SampleJSONBody() string {
	parts := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		if f.Optional {
			continue
		}
		parts = append(parts, fmt.Sprintf("%q:%s", f.JSON, f.SampleJSON()))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Data is passed to every template.
type Data struct {
	Project
	Mod Module
	Res Resource
	// Generic named artifact (command, query, event, port, contract...).
	Name      string // Pascal
	NameSnake string
	NameHuman string
	Fields    []fields.Field
	Extra     map[string]string
	Timestamp string
}

// HasTimeField reports whether any field is a time.
func (d Data) HasTimeField() bool {
	for _, f := range d.Fields {
		if f.IsTime() {
			return true
		}
	}
	return false
}
