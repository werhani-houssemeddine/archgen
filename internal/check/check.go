// Package check enforces the architecture rules of a modular monolith by analyzing
// Go imports:
//
//   - layer direction inside a module (domain ← application ← interfaces; infrastructure
//     → application/domain; nothing depends on infrastructure except the module root);
//   - technology bans per layer (no HTTP/SQL/drivers in the domain, etc.);
//   - module boundaries (another module is reachable only through its contract package);
//   - shared code never imports business modules.
package check

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Violation is one broken rule.
type Violation struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Import  string `json:"import"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s (import %q)", v.File, v.Line, v.Rule, v.Message, v.Import)
}

// Options configure a check.
type Options struct {
	Root         string
	Module       string // project go module path
	ModulesDir   string // e.g. internal/modules
	Kit          string // archkit import path
	IncludeTests bool
}

type location struct {
	module string // business module, "" if none
	layer  string // domain|application|infrastructure|interfaces|contract|root
	shared bool
}

var frameworkPrefixes = []string{
	"github.com/lib/pq", "github.com/jackc/pgx", "gorm.io/", "github.com/jmoiron/sqlx", "entgo.io/", "github.com/uptrace/bun",
	"github.com/gin-gonic/", "github.com/labstack/echo", "github.com/gofiber/", "github.com/go-chi/", "github.com/gorilla/mux",
	"google.golang.org/grpc", "github.com/redis/", "github.com/go-redis/", "go.mongodb.org/", "github.com/segmentio/kafka-go",
	"github.com/IBM/sarama", "github.com/nats-io/", "github.com/rabbitmq/", "github.com/aws/aws-sdk-go", "cloud.google.com/go/",
}

// Run analyzes every Go file under Root.
func Run(o Options) ([]Violation, error) {
	modulesPrefix := o.Module + "/" + filepath.ToSlash(o.ModulesDir) + "/"
	sharedPrefix := o.Module + "/internal/shared/"
	kitBans := func(pkgs ...string) []string {
		out := make([]string, len(pkgs))
		for i, p := range pkgs {
			out[i] = o.Kit + "/" + p
		}
		return out
	}
	techBans := map[string][]string{
		"domain": append(append([]string{"net/http", "database/sql", "os/exec"}, kitBans(
			"pg", "httpx", "cqrs", "events", "logx", "config", "lifecycle", "security", "cache", "mail", "resilience", "authz", "app")...),
			sharedPrefix+"database", sharedPrefix+"http", sharedPrefix+"logging", sharedPrefix+"observability"),
		"application": append(append([]string{"net/http", "database/sql"}, kitBans("pg", "httpx", "cache", "mail", "security", "lifecycle", "config")...),
			sharedPrefix+"database", sharedPrefix+"http"),
		"interfaces": append(append([]string{"database/sql"}, kitBans("pg", "cache", "mail")...), sharedPrefix+"database"),
		"contract":   append([]string{"net/http", "database/sql"}, kitBans("pg", "httpx", "cqrs", "events", "cache", "mail", "security")...),
	}
	// own-module layers each layer must not import
	layerBans := map[string][]string{
		"domain":         {"application", "infrastructure", "interfaces", "contract", "root"},
		"application":    {"infrastructure", "interfaces", "root"},
		"interfaces":     {"infrastructure", "root"},
		"infrastructure": {"interfaces", "root"},
		"contract":       {"domain", "application", "infrastructure", "interfaces", "root"},
	}

	var out []Violation
	err := filepath.WalkDir(o.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != o.Root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || (!o.IncludeTests && strings.HasSuffix(name, "_test.go")) {
			return nil
		}
		rel, _ := filepath.Rel(o.Root, path)
		rel = filepath.ToSlash(rel)
		loc := locate(rel, filepath.ToSlash(o.ModulesDir))
		if loc.module == "" && !loc.shared {
			return nil // cmd/, internal/app (composition root), migrations...
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not our job: the compiler reports syntax errors
		}
		for _, imp := range f.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value)
			line := fset.Position(imp.Pos()).Line
			add := func(rule, msg string) {
				out = append(out, Violation{File: rel, Line: line, Import: ip, Rule: rule, Message: msg})
			}
			if loc.shared {
				if strings.HasPrefix(ip, modulesPrefix) {
					add("shared-no-business", "shared code must not depend on business modules")
				}
				continue
			}
			// cross-module and intra-module rules
			if strings.HasPrefix(ip, modulesPrefix) {
				rest := strings.TrimPrefix(ip, modulesPrefix)
				target, sub, _ := strings.Cut(rest, "/")
				targetLayer := strings.SplitN(sub, "/", 2)[0]
				if sub == "" {
					targetLayer = "root"
				}
				if target != loc.module {
					if targetLayer != "contract" {
						add("module-boundary", fmt.Sprintf("module %q may only use module %q through %s/contract", loc.module, target, target))
					} else if loc.layer == "domain" {
						add("domain-isolation", "the domain must not depend on other modules (map contract data in the application layer)")
					}
					continue
				}
				for _, banned := range layerBans[loc.layer] {
					if targetLayer == banned {
						add("layer-direction", fmt.Sprintf("%s must not depend on %s", loc.layer, describe(banned)))
					}
				}
				continue
			}
			for _, banned := range techBans[loc.layer] {
				if ip == banned || strings.HasPrefix(ip, banned+"/") {
					add("layer-technology", fmt.Sprintf("%s must not import %s", loc.layer, ip))
				}
			}
			if loc.layer == "domain" || loc.layer == "application" || loc.layer == "contract" {
				for _, fw := range frameworkPrefixes {
					if strings.HasPrefix(ip, fw) {
						add("layer-technology", fmt.Sprintf("%s must not import infrastructure library %s (put it behind a port)", loc.layer, ip))
					}
				}
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, err
}

func describe(layer string) string {
	if layer == "root" {
		return "the module root package (wiring)"
	}
	return "its own " + layer + " layer"
}

func locate(rel, modulesDir string) location {
	if strings.HasPrefix(rel, "internal/shared/") {
		return location{shared: true}
	}
	if !strings.HasPrefix(rel, modulesDir+"/") {
		return location{}
	}
	parts := strings.Split(strings.TrimPrefix(rel, modulesDir+"/"), "/")
	if len(parts) < 2 {
		return location{}
	}
	loc := location{module: parts[0], layer: "root"}
	if len(parts) > 2 {
		switch parts[1] {
		case "domain", "application", "infrastructure", "interfaces", "contract":
			loc.layer = parts[1]
		default:
			loc.layer = "other"
		}
	}
	return loc
}

// Exists reports whether path exists (helper for callers).
func Exists(path string) bool { _, err := os.Stat(path); return err == nil }
