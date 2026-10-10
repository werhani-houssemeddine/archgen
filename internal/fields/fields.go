// Package fields parses the field DSL used by generators:
//
//	"title:string, description:text:optional, email:email:unique, price:float,
//	 status:enum(draft|published):index, startsAt:time, ownerId:uuid"
//
// Format: name:type[:modifier[:modifier...]]
// Types:   string, text, email, url, uuid, int, int64, float, bool, time, enum(a|b|c)
// Modifiers: optional (nullable, not required), unique, index
package fields

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/werhani-houssemeddine/archgen/internal/naming"
)

// Field is one parsed attribute with everything templates need.
type Field struct {
	Raw      string
	Name     string // Pascal: "StartsAt"
	Camel    string // Go unexported: "startsAt"
	JSON     string // "startsAt"
	Snake    string // SQL column: "starts_at"
	Kind     string // string|text|email|url|uuid|int|int64|float|bool|time|enum
	Enum     []string
	Optional bool
	Unique   bool
	Index    bool
}

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// Reserved names are managed by the generator (or clash with AggregateRoot methods).
var reserved = map[string]bool{
	"id": true, "created_at": true, "updated_at": true, "version": true,
	"record": true, "restore": true, "snapshot": true, "update": true, "apply": true,
	"mark_persisted": true, "pending_events": true, "pull_events": true, "mark_deleted": true,
}

var kinds = map[string]bool{"string": true, "text": true, "email": true, "url": true, "uuid": true, "int": true, "int64": true, "float": true, "bool": true, "time": true}

// Parse parses a comma-separated field list.
func Parse(spec string) ([]Field, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var out []Field
	seen := map[string]bool{}
	for _, raw := range splitTop(spec) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		f, err := parseOne(raw)
		if err != nil {
			return nil, err
		}
		if seen[f.Snake] {
			return nil, fmt.Errorf("field %q is declared twice", f.Snake)
		}
		seen[f.Snake] = true
		out = append(out, f)
	}
	return out, nil
}

// splitTop splits on commas that are not inside parentheses.
func splitTop(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

func parseOne(raw string) (Field, error) {
	segs := strings.Split(raw, ":")
	if len(segs) < 2 {
		return Field{}, fmt.Errorf("field %q: expected name:type", raw)
	}
	name, typ := strings.TrimSpace(segs[0]), strings.TrimSpace(segs[1])
	if !nameRe.MatchString(name) {
		return Field{}, fmt.Errorf("field %q: invalid name", raw)
	}
	snake := naming.Snake(name)
	if reserved[snake] {
		return Field{}, fmt.Errorf("field %q: %q is reserved (managed by the generator)", raw, snake)
	}
	if naming.IsGoKeyword(naming.Camel(name)) {
		return Field{}, fmt.Errorf("field %q: %q is a Go keyword", raw, name)
	}
	f := Field{Raw: raw, Name: naming.Pascal(name), Camel: naming.Camel(name), JSON: naming.JSONName(name), Snake: snake}
	switch {
	case strings.HasPrefix(typ, "enum(") && strings.HasSuffix(typ, ")"):
		f.Kind = "enum"
		for _, v := range strings.Split(typ[5:len(typ)-1], "|") {
			v = strings.TrimSpace(v)
			if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(v) {
				return Field{}, fmt.Errorf("field %q: enum values must be lowercase snake_case (got %q)", raw, v)
			}
			f.Enum = append(f.Enum, v)
		}
		if len(f.Enum) < 2 {
			return Field{}, fmt.Errorf("field %q: enum needs at least 2 values", raw)
		}
	case kinds[typ]:
		f.Kind = typ
	default:
		return Field{}, fmt.Errorf("field %q: unknown type %q (string, text, email, url, uuid, int, int64, float, bool, time, enum(a|b))", raw, typ)
	}
	for _, m := range segs[2:] {
		switch strings.TrimSpace(m) {
		case "optional", "nullable":
			f.Optional = true
		case "unique":
			f.Unique = true
		case "index":
			f.Index = true
		case "":
		default:
			return Field{}, fmt.Errorf("field %q: unknown modifier %q (optional, unique, index)", raw, m)
		}
	}
	if f.Kind == "bool" && f.Optional {
		return Field{}, fmt.Errorf("field %q: optional booleans are ambiguous; use a default false bool or an enum", raw)
	}
	return f, nil
}

// BaseGoType returns the non-pointer Go type.
func (f Field) BaseGoType() string {
	switch f.Kind {
	case "int":
		return "int"
	case "int64":
		return "int64"
	case "float":
		return "float64"
	case "bool":
		return "bool"
	case "time":
		return "time.Time"
	default:
		return "string"
	}
}

// GoType returns the Go type (pointer when optional).
func (f Field) GoType() string {
	if f.Optional {
		return "*" + f.BaseGoType()
	}
	return f.BaseGoType()
}

// IsString reports string-backed kinds.
func (f Field) IsString() bool {
	switch f.Kind {
	case "string", "text", "email", "url", "uuid", "enum":
		return true
	}
	return false
}

// IsTime reports time fields.
func (f Field) IsTime() bool { return f.Kind == "time" }

// SQLType returns the PostgreSQL column type.
func (f Field) SQLType() string {
	switch f.Kind {
	case "int":
		return "integer"
	case "int64":
		return "bigint"
	case "float":
		return "double precision"
	case "bool":
		return "boolean"
	case "time":
		return "timestamptz"
	case "uuid":
		return "uuid"
	default:
		return "text"
	}
}

// SQLColumn returns the column definition (without trailing comma).
func (f Field) SQLColumn() string {
	col := fmt.Sprintf("%s %s", f.Snake, f.SQLType())
	if !f.Optional {
		col += " NOT NULL"
	}
	if f.Kind == "bool" {
		col += " DEFAULT false"
	}
	if f.Kind == "enum" {
		quoted := make([]string, len(f.Enum))
		for i, v := range f.Enum {
			quoted[i] = "'" + v + "'"
		}
		col += fmt.Sprintf(" CHECK (%s IN (%s))", f.Snake, strings.Join(quoted, ", "))
	}
	return col
}

// ValidateLine returns the validation statement for value expression `expr` (e.g. "p.Title").
func (f Field) ValidateLine(expr string) string {
	path := f.JSON
	switch {
	case f.IsString() && f.Optional:
		rule := f.stringRules()
		if rule == "" {
			return ""
		}
		return fmt.Sprintf("v.OptionalString(%q, %s)%s", path, expr, rule)
	case f.IsString():
		return fmt.Sprintf("v.String(%q, %s).Required()%s", path, expr, f.stringRules())
	case f.IsTime() && !f.Optional:
		return fmt.Sprintf("v.Time(%q, %s).Required()", path, expr)
	}
	return ""
}

func (f Field) stringRules() string {
	switch f.Kind {
	case "string":
		return ".Max(255)"
	case "text":
		return ".Max(10000)"
	case "email":
		return ".Max(254).Email()"
	case "url":
		return ".Max(2048).URL()"
	case "uuid":
		return ".UUID()"
	case "enum":
		q := make([]string, len(f.Enum))
		for i, v := range f.Enum {
			q[i] = fmt.Sprintf("%q", v)
		}
		return ".OneOf(" + strings.Join(q, ", ") + ")"
	}
	return ""
}

// SampleValue returns a valid Go literal for tests (pointer-wrapped when optional is false).
func (f Field) SampleValue() string {
	switch f.Kind {
	case "string", "text":
		return `"sample ` + f.JSON + `"`
	case "email":
		return `"user@example.com"`
	case "url":
		return `"https://example.com"`
	case "uuid":
		return `"01890a5d-ac96-774b-bcce-b302099a8057"`
	case "enum":
		return fmt.Sprintf("%q", f.Enum[0])
	case "int", "int64":
		return "1"
	case "float":
		return "1.5"
	case "bool":
		return "true"
	case "time":
		return "time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)"
	}
	return `""`
}

// SampleJSON returns a valid JSON value for HTTP tests.
func (f Field) SampleJSON() string {
	switch f.Kind {
	case "int", "int64":
		return "1"
	case "float":
		return "1.5"
	case "bool":
		return "true"
	case "time":
		return `"2030-01-01T00:00:00Z"`
	default:
		v := f.SampleValue()
		return v
	}
}

// Required reports fields whose absence must fail validation.
func (f Field) Required() bool { return !f.Optional && (f.IsString() || f.IsTime()) }

// Searchable reports fields used by the generated text search.
func (f Field) Searchable() bool { return f.Kind == "string" || f.Kind == "text" || f.Kind == "email" }
