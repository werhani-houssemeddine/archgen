// Package cli implements the archgen commands.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/werhani-houssemeddine/archgen/internal/gen"
	"github.com/werhani-houssemeddine/archgen/internal/naming"
	"github.com/werhani-houssemeddine/archgen/internal/project"
	"github.com/werhani-houssemeddine/archgen/internal/tmpl"
)

// Version of archgen.
const Version = "1.0.0"

type command struct {
	name    string
	summary string
	usage   string
	run     func(c *ctx, args []string) error
}

type ctx struct {
	out, errOut io.Writer
	dir         string
	dryRun      bool
	force       bool
	now         func() time.Time
}

var commands []command

func init() {
	commands = []command{
		{"new", "create a new modular-monolith project", "new <name> [--module github.com/acme/backend] [--kit <archkit import path>] [--kit-replace <local path>] [--driver pgx|pq] [--migrations timestamp|sequential]", runNew},
		{"module", "add a business module (layers, contract, schema migration, app wiring)", "module <name>", runModule},
		{"resource", "add an aggregate with its full CRUD slice (domain → HTTP, migration, tests)", `resource <module> <Name> --fields "title:string:unique, price:float, status:enum(draft|live)" [--no-http]`, runResource},
		{"command", "add a write use case", `command <module> <Name> [--fields "..."]`, runCommand},
		{"query", "add a read use case", `query <module> <Name> [--fields "..."]`, runQuery},
		{"event", "add a domain event (or a public integration event with --public)", `event <module> <Name> [--fields "..."] [--public]`, runEvent},
		{"subscribe", "make a module react to another module's public event", "subscribe <module> <otherModule>.<Event>", runSubscribe},
		{"contract", "add a public read contract other modules can depend on", "contract <module> <Name> [--resource <Aggregate>]", runContract},
		{"use", "inject another module's public contract into a module (Deps + composition root)", "use <module> <otherModule>.<Contract>", runUse},
		{"port", "add an application port (interface) with an infrastructure adapter", "port <module> <Name> [--adapter <dir>]", runPort},
		{"migration", "add an empty SQL migration", "migration <module|-> <name>", runMigration},
		{"check", "verify the architecture rules (layers, module boundaries)", "check [--tests] [--json]", runCheck},
		{"list", "list modules, aggregates, use cases and contracts", "list", runList},
		{"version", "print the version", "version", func(c *ctx, _ []string) error { fmt.Fprintln(c.out, "archgen", Version); return nil }},
	}
}

// Run executes archgen with os-style args (without the program name) and returns an exit code.
func Run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		if len(args) > 1 {
			if cmd := find(args[1]); cmd != nil {
				fmt.Fprintf(out, "Usage: archgen %s\n\n%s\n", cmd.usage, cmd.summary)
				return 0
			}
		}
		usage(out)
		return 0
	}
	cmd := find(args[0])
	if cmd == nil {
		fmt.Fprintf(errOut, "archgen: unknown command %q\n\n", args[0])
		usage(errOut)
		return 2
	}
	c := &ctx{out: out, errOut: errOut, now: time.Now}
	if err := cmd.run(c, args[1:]); err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(errOut, "archgen %s: %s\nUsage: archgen %s\n", cmd.name, ue.msg, cmd.usage)
			return 2
		}
		var ce checkFailed
		if errors.As(err, &ce) {
			return 1
		}
		fmt.Fprintf(errOut, "archgen %s: %v\n", cmd.name, err)
		return 1
	}
	return 0
}

func find(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "archgen %s: scaffolding for Go modular monoliths (Clean Architecture + archkit)\n\nUsage: archgen <command> [arguments] [flags]\n\nCommands:\n", Version)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, "\nCommon flags: --dir <path> (project directory, default .), --dry-run, --force (overwrite existing files)\nRun `archgen help <command>` for details.\n")
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

type checkFailed struct{}

func (checkFailed) Error() string { return "architecture check failed" }

// parse parses flags placed anywhere among positional arguments.
func (c *ctx) parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.StringVar(&c.dir, "dir", ".", "project directory")
	fs.BoolVar(&c.dryRun, "dry-run", false, "show what would be generated without writing")
	fs.BoolVar(&c.force, "force", false, "overwrite existing files")
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usagef("%v", err)
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func (c *ctx) load() (*project.Project, *gen.Writer, error) {
	p, err := project.Load(c.dir)
	if err != nil {
		return nil, nil, err
	}
	w := gen.New(p.Root, c.dryRun, c.force)
	w.LocalPrefix = p.Module
	return p, w, nil
}

func (c *ctx) report(w *gen.Writer, next ...string) {
	if c.dryRun {
		fmt.Fprintln(c.out, "Dry run, nothing written:")
	}
	fmt.Fprint(c.out, w.Summary())
	if len(next) > 0 && !c.dryRun {
		fmt.Fprintln(c.out, "\nNext steps:")
		for _, n := range next {
			fmt.Fprintln(c.out, "  "+n)
		}
	}
}

func projectData(p *project.Project) tmpl.Project {
	d := tmpl.Project{
		AppName: naming.Kebab(filepath.Base(p.Root)), Module: p.Module, Kit: p.Kit, GoVersion: "1.24", Driver: p.Driver,
		APIPrefix: p.APIPrefix, ModulesDir: filepath.ToSlash(p.ModulesDir),
	}
	switch p.Driver {
	case "pq":
		d.DriverImport, d.DriverName = "github.com/lib/pq", "postgres"
	default:
		d.DriverImport, d.DriverName = "github.com/jackc/pgx/v5/stdlib", "pgx"
	}
	return d
}

func render(w *gen.Writer, rel, name string, data any) error {
	b, err := tmpl.Render(name, data)
	if err != nil {
		return err
	}
	return w.Write(filepath.FromSlash(rel), b)
}

/* ------------------------------------------------------------------ */
/* Names                                                               */
/* ------------------------------------------------------------------ */

var identRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// Module package names that would clash with imports in generated wiring code.
var reservedModules = map[string]bool{
	"app": true, "http": true, "config": true, "domain": true, "events": true, "pg": true, "cqrs": true, "httpx": true,
	"log": true, "slog": true, "context": true, "time": true, "errors": true, "shared": true, "kernel": true, "internal": true,
	"clock": true, "authz": true, "resilience": true, "jwtauth": true, "lifecycle": true, "logx": true, "migrations": true,
	"database": true, "logging": true, "observability": true, "contract": true, "commands": true, "queries": true, "ports": true,
	"persistence": true, "main": true, "test": true, "public": true, "platform": true,
}

func validateModuleName(name string) error {
	if !identRe.MatchString(name) {
		return usagef("invalid module name %q (letters, digits, - and _)", name)
	}
	pkg := naming.Package(name)
	if reservedModules[pkg] || naming.IsGoKeyword(pkg) {
		return usagef("module name %q is reserved; pick a business name (e.g. %q)", name, name+"s")
	}
	return nil
}

func validateTypeName(kind, name string) error {
	if !identRe.MatchString(name) {
		return usagef("invalid %s name %q", kind, name)
	}
	return nil
}

func requireModule(p *project.Project, name string) (tmpl.Module, error) {
	m := tmpl.NewModule(name)
	if !p.HasModule(m.Pkg) {
		mods, _ := p.Modules()
		return m, fmt.Errorf("module %q does not exist (existing: %s); create it with `archgen module %s`", m.Pkg, strings.Join(mods, ", "), name)
	}
	return m, nil
}

/* ------------------------------------------------------------------ */
/* Migrations                                                          */
/* ------------------------------------------------------------------ */

var migRe = regexp.MustCompile(`^(\d+)[_-]`)

// nextMigration returns a new migration file path relative to the root. Timestamp
// versions avoid merge conflicts between branches; collisions within the same second
// (several migrations in one command) are resolved by incrementing the timestamp.
func (c *ctx) nextMigration(p *project.Project, w *gen.Writer, name string) string {
	dir := filepath.Join(p.Root, p.MigrationsDir)
	existing := map[string]bool{}
	var maxSeq int
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if m := migRe.FindStringSubmatch(e.Name()); m != nil {
			existing[m[1]] = true
			if n, err := strconv.Atoi(m[1]); err == nil && len(m[1]) < 10 && n > maxSeq {
				maxSeq = n
			}
		}
	}
	for _, a := range w.Actions { // created earlier in this run (dry-run aware)
		if m := migRe.FindStringSubmatch(filepath.Base(a.Path)); m != nil && strings.HasPrefix(filepath.ToSlash(a.Path), p.MigrationsDir+"/") {
			existing[m[1]] = true
			if n, err := strconv.Atoi(m[1]); err == nil && len(m[1]) < 10 && n > maxSeq {
				maxSeq = n
			}
		}
	}
	var version string
	if p.Migrations == "sequential" {
		version = fmt.Sprintf("%04d", maxSeq+1)
	} else {
		t := c.now().UTC()
		for {
			version = t.Format("20060102150405")
			if !existing[version] {
				break
			}
			t = t.Add(time.Second)
		}
	}
	return filepath.Join(p.MigrationsDir, version+"_"+naming.Snake(name)+".sql")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatGo(src string) ([]byte, error) {
	b, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("invalid Go after edit: %w", err)
	}
	return b, nil
}
