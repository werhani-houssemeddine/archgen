package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/werhani-houssemeddine/archgen/internal/fields"
	"github.com/werhani-houssemeddine/archgen/internal/gen"
	"github.com/werhani-houssemeddine/archgen/internal/naming"
	"github.com/werhani-houssemeddine/archgen/internal/project"
	"github.com/werhani-houssemeddine/archgen/internal/tmpl"
)

type namedArgs struct {
	p      *project.Project
	w      *gen.Writer
	mod    tmpl.Module
	name   string
	fields []fields.Field
	extra  *flag.FlagSet
}

// parseNamed handles "<module> <Name> [--fields ...]" commands.
func (c *ctx) parseNamed(cmd string, args []string, withFields bool, setup func(fs *flag.FlagSet)) (*namedArgs, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var spec *string
	if withFields {
		spec = fs.String("fields", "", "field list")
	}
	if setup != nil {
		setup(fs)
	}
	pos, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 2 {
		return nil, usagef("expected <module> <Name>")
	}
	if err := validateTypeName(cmd, pos[1]); err != nil {
		return nil, err
	}
	var fl []fields.Field
	if spec != nil {
		if fl, err = fields.Parse(*spec); err != nil {
			return nil, usagef("%v", err)
		}
	}
	p, w, err := c.load()
	if err != nil {
		return nil, err
	}
	m, err := requireModule(p, pos[0])
	if err != nil {
		return nil, err
	}
	return &namedArgs{p: p, w: w, mod: m, name: naming.Pascal(pos[1]), fields: fl, extra: fs}, nil
}

func (a *namedArgs) data() tmpl.Data {
	return tmpl.Data{Project: projectData(a.p), Mod: a.mod, Name: a.name, NameSnake: naming.Snake(a.name), NameHuman: naming.Human(a.name), Fields: a.fields, Extra: map[string]string{}}
}

func (a *namedArgs) base(sub ...string) string {
	return filepath.Join(append([]string{a.p.ModulesDir, a.mod.Pkg}, sub...)...)
}

func (a *namedArgs) moduleFile() string { return a.base("module.go") }

func (a *namedArgs) ensureNew(rel string, force bool) error {
	if a.w.Exists(rel) && !force {
		return fmt.Errorf("%s already exists", filepath.ToSlash(rel))
	}
	return nil
}

/* ------------------------------------------------------------------ */

func runCommand(c *ctx, args []string) error {
	a, err := c.parseNamed("command", args, true, nil)
	if err != nil {
		return err
	}
	rel := a.base("application", "commands", naming.Snake(a.name)+".go")
	if err := a.ensureNew(rel, c.force); err != nil {
		return err
	}
	if err := render(a.w, rel, "x_command.go.tmpl", a.data()); err != nil {
		return err
	}
	if err := a.w.Insert(a.moduleFile(), "wire", fmt.Sprintf("commands.Register%s(d.Mediator)", a.name)); err != nil {
		return err
	}
	if err := a.w.EnsureImport(a.moduleFile(), a.p.ModuleImport(a.mod.Pkg, "application", "commands"), ""); err != nil {
		return err
	}
	c.report(a.w, "implement "+filepath.ToSlash(rel)+" (inject repositories through New"+a.name+"Handler and module.go)",
		fmt.Sprintf("call it with cqrs.Send[struct{}](ctx, mediator, commands.%s{...}) from an HTTP handler", a.name))
	return nil
}

func runQuery(c *ctx, args []string) error {
	a, err := c.parseNamed("query", args, true, nil)
	if err != nil {
		return err
	}
	rel := a.base("application", "queries", naming.Snake(a.name)+".go")
	if err := a.ensureNew(rel, c.force); err != nil {
		return err
	}
	if err := render(a.w, rel, "x_query.go.tmpl", a.data()); err != nil {
		return err
	}
	if err := a.w.Insert(a.moduleFile(), "wire", fmt.Sprintf("queries.Register%s(d.Mediator)", a.name)); err != nil {
		return err
	}
	if err := a.w.EnsureImport(a.moduleFile(), a.p.ModuleImport(a.mod.Pkg, "application", "queries"), ""); err != nil {
		return err
	}
	c.report(a.w, "implement "+filepath.ToSlash(rel))
	return nil
}

func runEvent(c *ctx, args []string) error {
	var public *bool
	a, err := c.parseNamed("event", args, true, func(fs *flag.FlagSet) {
		public = fs.Bool("public", false, "declare a public integration event in the contract package")
	})
	if err != nil {
		return err
	}
	rel, name := a.base("domain", naming.Snake(a.name)+"_event.go"), "x_event_domain.go.tmpl"
	if *public {
		rel, name = a.base("contract", naming.Snake(a.name)+"_event.go"), "x_event_public.go.tmpl"
	}
	if err := a.ensureNew(rel, c.force); err != nil {
		return err
	}
	if err := render(a.w, rel, name, a.data()); err != nil {
		return err
	}
	next := []string{"record it from an aggregate method: agg.Record(" + a.name + "Event.New(ctx, " + a.name + "{...}))"}
	if *public {
		next = []string{
			"publish it from a command handler before saving: agg.Record(contract." + a.name + "Event.New(ctx, contract." + a.name + "{...}))",
			fmt.Sprintf("subscribe from another module: archgen subscribe <module> %s.%s", a.mod.Pkg, a.name),
		}
	}
	c.report(a.w, next...)
	return nil
}

var eventRefRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)\.([A-Za-z][A-Za-z0-9_]*)$`)

func runSubscribe(c *ctx, args []string) error {
	fs := flag.NewFlagSet("subscribe", flag.ContinueOnError)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("expected <module> <otherModule>.<Event>")
	}
	ref := eventRefRe.FindStringSubmatch(pos[1])
	if ref == nil {
		return usagef("event reference must look like users.UserRegistered")
	}
	p, w, err := c.load()
	if err != nil {
		return err
	}
	consumer, err := requireModule(p, pos[0])
	if err != nil {
		return err
	}
	provider, err := requireModule(p, ref[1])
	if err != nil {
		return err
	}
	if provider.Pkg == consumer.Pkg {
		return usagef("a module subscribes to its own domain events directly; subscribe is for other modules' public events")
	}
	event := naming.Pascal(ref[2])
	if !contractDeclares(p, provider.Pkg, event+"Event") {
		return fmt.Errorf("%s/contract does not declare %sEvent; create it with `archgen event %s %s --public`", provider.Pkg, event, provider.Pkg, event)
	}
	handler := "On" + provider.Pascal + event
	alias := provider.Pkg + "contract"
	d := tmpl.Data{Project: projectData(p), Mod: consumer, Name: handler, NameSnake: naming.Snake(handler), Extra: map[string]string{
		"Provider": provider.Pkg, "Event": event, "ProviderAlias": alias, "ProviderContract": p.ModuleImport(provider.Pkg, "contract"),
	}}
	base := filepath.Join(p.ModulesDir, consumer.Pkg)
	rel := filepath.Join(base, "application", "eventhandlers", naming.Snake(handler)+".go")
	if w.Exists(rel) && !c.force {
		return fmt.Errorf("%s already exists", filepath.ToSlash(rel))
	}
	if err := render(w, rel, "x_subscriber.go.tmpl", d); err != nil {
		return err
	}
	mod := filepath.Join(base, "module.go")
	snippet := fmt.Sprintf("events.On(bus, %s.%sEvent, %q, eventhandlers.New%s(m.deps.Logger).Handle)", alias, event, consumer.Snake+"."+naming.Snake(handler), handler)
	if err := w.Insert(mod, "subscriptions", snippet); err != nil {
		return err
	}
	for _, imp := range [][2]string{
		{p.Kit + "/events", ""},
		{p.ModuleImport(provider.Pkg, "contract"), alias},
		{p.ModuleImport(consumer.Pkg, "application", "eventhandlers"), ""},
	} {
		if err := w.EnsureImport(mod, imp[0], imp[1]); err != nil {
			return err
		}
	}
	c.report(w, "implement "+filepath.ToSlash(rel)+" (idempotently: delivery is at-least-once)")
	return nil
}

// contractDeclares reports whether a module's contract package declares an identifier.
func contractDeclares(p *project.Project, pkg, ident string) bool {
	dir := filepath.Join(p.ModuleDir(pkg), "contract")
	entries, _ := os.ReadDir(dir)
	re := regexp.MustCompile(`(?m)^\s*(var\s+)?` + regexp.QuoteMeta(ident) + `\s*=`)
	for _, e := range entries {
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && re.Match(b) {
			return true
		}
	}
	return false
}

func runContract(c *ctx, args []string) error {
	var resource *string
	a, err := c.parseNamed("contract", args, false, func(fs *flag.FlagSet) {
		resource = fs.String("resource", "", "aggregate exposed by the contract (generates Get<Aggregate>)")
	})
	if err != nil {
		return err
	}
	d := a.data()
	adapter := naming.Camel(a.name) + "Adapter"
	d.Extra["Adapter"] = adapter
	repoExpr := ""
	if *resource != "" {
		fl, err := readAggregateFields(a.p, a.mod.Pkg, naming.Pascal(*resource))
		if err != nil {
			return err
		}
		d.Res = tmpl.NewResource(a.mod, *resource, fl)
		repoExpr = "repo: m." + d.Res.Camel + "Repo"
		if b, _ := os.ReadFile(filepath.Join(a.p.Root, a.moduleFile())); !strings.Contains(string(b), d.Res.Camel+"Repo ") {
			return fmt.Errorf("module.go has no %sRepo field; was the aggregate created with `archgen resource`?", d.Res.Camel)
		}
	}
	rel := a.base("contract", naming.Snake(a.name)+".go")
	if err := a.ensureNew(rel, c.force); err != nil {
		return err
	}
	if err := render(a.w, rel, "x_contract.go.tmpl", d); err != nil {
		return err
	}
	if err := render(a.w, a.base(naming.Snake(a.name)+"_adapter.go"), "x_contract_adapter.go.tmpl", d); err != nil {
		return err
	}
	accessor := fmt.Sprintf("// %s returns the module's public %s contract.\nfunc (m *Module) %s() contract.%s { return %s{%s} }", a.name, a.name, a.name, a.name, adapter, repoExpr)
	if err := a.w.Insert(a.moduleFile(), "contract-accessors", accessor); err != nil {
		return err
	}
	if err := a.w.EnsureImport(a.moduleFile(), a.p.ModuleImport(a.mod.Pkg, "contract"), ""); err != nil {
		return err
	}
	c.report(a.w, fmt.Sprintf("let another module use it: archgen use <module> %s.%s", a.mod.Pkg, a.name))
	return nil
}

// readAggregateFields recovers the fields of a generated aggregate from its Params struct.
func readAggregateFields(p *project.Project, pkg, name string) ([]fields.Field, error) {
	path := filepath.Join(p.ModuleDir(pkg), "domain", naming.Snake(name)+".go")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aggregate %s not found in module %s (%s)", name, pkg, filepath.ToSlash(path))
	}
	re := regexp.MustCompile(`(?s)type ` + name + `Params struct \{(.*?)\n\}`)
	m := re.FindSubmatch(b)
	if m == nil {
		return nil, fmt.Errorf("%s: %sParams struct not found", path, name)
	}
	var out []fields.Field
	for _, line := range strings.Split(string(m[1]), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || strings.HasPrefix(parts[0], "//") {
			continue
		}
		f := fields.Field{Name: parts[0], Camel: naming.Camel(parts[0]), JSON: naming.JSONName(parts[0]), Snake: naming.Snake(parts[0])}
		t := parts[1]
		if strings.HasPrefix(t, "*") {
			f.Optional, t = true, t[1:]
		}
		switch t {
		case "int":
			f.Kind = "int"
		case "int64":
			f.Kind = "int64"
		case "float64":
			f.Kind = "float"
		case "bool":
			f.Kind = "bool"
		case "time.Time":
			f.Kind = "time"
		default:
			f.Kind = "string"
		}
		out = append(out, f)
	}
	return out, nil
}

func runPort(c *ctx, args []string) error {
	var adapterDir *string
	a, err := c.parseNamed("port", args, false, func(fs *flag.FlagSet) {
		adapterDir = fs.String("adapter", "adapters", "infrastructure sub-directory of the adapter (e.g. security, external, messaging)")
	})
	if err != nil {
		return err
	}
	pkg := naming.Package(*adapterDir)
	if pkg == "" || reservedModules[pkg] && pkg != "persistence" {
		return usagef("invalid adapter directory %q", *adapterDir)
	}
	d := a.data()
	d.Extra["AdapterPkg"] = pkg
	portRel := a.base("application", "ports", naming.Snake(a.name)+".go")
	if err := a.ensureNew(portRel, c.force); err != nil {
		return err
	}
	if err := render(a.w, portRel, "x_port.go.tmpl", d); err != nil {
		return err
	}
	adapterRel := a.base("infrastructure", pkg, naming.Snake(a.name)+".go")
	if err := render(a.w, adapterRel, "x_adapter.go.tmpl", d); err != nil {
		return err
	}
	c.report(a.w,
		"define the port's methods in "+filepath.ToSlash(portRel)+" and implement them in "+filepath.ToSlash(adapterRel),
		fmt.Sprintf("inject %s.New%s() into the handlers that need it from module.go", pkg, a.name))
	return nil
}

/* ------------------------------------------------------------------ */
/* use: wire another module's contract into a consumer                 */
/* ------------------------------------------------------------------ */

func runUse(c *ctx, args []string) error {
	fs := flag.NewFlagSet("use", flag.ContinueOnError)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("expected <consumerModule> <providerModule>.<Contract>")
	}
	ref := eventRefRe.FindStringSubmatch(pos[1])
	if ref == nil {
		return usagef("contract reference must look like users.UserReader")
	}
	p, w, err := c.load()
	if err != nil {
		return err
	}
	consumer, err := requireModule(p, pos[0])
	if err != nil {
		return err
	}
	provider, err := requireModule(p, ref[1])
	if err != nil {
		return err
	}
	if consumer.Pkg == provider.Pkg {
		return usagef("a module does not consume its own contract")
	}
	contractName := naming.Pascal(ref[2])
	if !contractInterface(p, provider.Pkg, contractName) {
		return fmt.Errorf("%s/contract does not declare interface %s; create it with `archgen contract %s %s`", provider.Pkg, contractName, provider.Pkg, contractName)
	}
	alias := provider.Pkg + "contract"
	field := contractName
	mod := filepath.Join(p.ModulesDir, consumer.Pkg, "module.go")
	if err := w.Insert(mod, "deps", fmt.Sprintf("%s %s.%s", field, alias, contractName)); err != nil {
		return err
	}
	if err := w.EnsureImport(mod, p.ModuleImport(provider.Pkg, "contract"), alias); err != nil {
		return err
	}
	// composition root: pass the provider's contract and make sure the provider is built first
	deps := filepath.Join("internal", "app", "dependencies.go")
	if err := wireContract(w, deps, consumer, provider, field, contractName); err != nil {
		return err
	}
	c.report(w, fmt.Sprintf("use m.deps.%s in %s handlers (pass it to New...Handler in module.go)", field, consumer.Pkg))
	return nil
}

func contractInterface(p *project.Project, pkg, name string) bool {
	dir := filepath.Join(p.ModuleDir(pkg), "contract")
	entries, _ := os.ReadDir(dir)
	re := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `\s+interface\b`)
	for _, e := range entries {
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && re.Match(b) {
			return true
		}
	}
	return false
}

func wireContract(w *gen.Writer, rel string, consumer, provider tmpl.Module, field, contractName string) error {
	b, err := os.ReadFile(filepath.Join(w.Root, rel))
	if err != nil {
		return fmt.Errorf("%s: %w (pass %s: ms.%s.%s() to %s.Deps manually)", rel, err, field, provider.Pascal, contractName, consumer.Pkg)
	}
	lines := strings.Split(string(b), "\n")
	consumerPrefix := fmt.Sprintf("ms.%s = %s.New(%s.Deps{", consumer.Pascal, consumer.Pkg, consumer.Pkg)
	providerPrefix := fmt.Sprintf("ms.%s = %s.New(", provider.Pascal, provider.Pkg)
	ci, pi := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, consumerPrefix) {
			ci = i
		}
		if strings.HasPrefix(t, providerPrefix) {
			pi = i
		}
	}
	if ci < 0 || pi < 0 {
		return fmt.Errorf("%s: could not find the wiring lines of %s and %s; pass %s: ms.%s.%s() to %s.Deps manually", rel, consumer.Pkg, provider.Pkg, field, provider.Pascal, contractName, consumer.Pkg)
	}
	entry := fmt.Sprintf("%s: ms.%s.%s()", field, provider.Pascal, contractName)
	if strings.Contains(lines[ci], entry) {
		w.Actions = append(w.Actions, gen.Action{Path: rel, Verb: "unchanged"})
		return nil
	}
	lines[ci] = strings.Replace(lines[ci], consumerPrefix, consumerPrefix+entry+", ", 1)
	if pi > ci { // provider must be constructed before the consumer
		line := lines[ci]
		lines = append(lines[:ci], lines[ci+1:]...)
		pi--
		lines = append(lines[:pi+1], append([]string{line}, lines[pi+1:]...)...)
	}
	out := strings.Join(lines, "\n")
	formatted, err := formatGo(out)
	if err != nil {
		return err
	}
	return w.Update(rel, formatted)
}

/* ------------------------------------------------------------------ */
/* list                                                                */
/* ------------------------------------------------------------------ */

func runList(c *ctx, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	if _, err := c.parse(fs, args); err != nil {
		return err
	}
	p, err := project.Load(c.dir)
	if err != nil {
		return err
	}
	mods, err := p.Modules()
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s (%s)\n", p.Module, filepath.ToSlash(p.ModulesDir))
	if len(mods) == 0 {
		fmt.Fprintln(c.out, "  no modules yet: archgen module <name>")
	}
	aggRe := regexp.MustCompile(`(?m)^type (\w+) struct \{\s*\n\s*ddd\.AggregateRoot`)
	typeRe := regexp.MustCompile(`(?m)^type (\w+) (struct|interface)`)
	evRe := regexp.MustCompile(`(?m)^\s*(?:var\s+)?(\w+Event)\s*=\s*ddd\.DefineEvent`)
	for _, m := range mods {
		dir := p.ModuleDir(m)
		fmt.Fprintf(c.out, "\n%s\n", m)
		show := func(label string, items []string) {
			if len(items) > 0 {
				fmt.Fprintf(c.out, "  %-12s %s\n", label, strings.Join(items, ", "))
			}
		}
		show("aggregates", grep(filepath.Join(dir, "domain"), aggRe, 1))
		show("commands", handlersIn(filepath.Join(dir, "application", "commands"), typeRe))
		show("queries", handlersIn(filepath.Join(dir, "application", "queries"), typeRe))
		show("ports", grep(filepath.Join(dir, "application", "ports"), regexp.MustCompile(`(?m)^type (\w+) interface`), 1))
		show("events", grep(filepath.Join(dir, "domain"), evRe, 1))
		show("contracts", grep(filepath.Join(dir, "contract"), regexp.MustCompile(`(?m)^type (\w+) interface`), 1))
		show("public events", grep(filepath.Join(dir, "contract"), evRe, 1))
		show("subscribers", grep(filepath.Join(dir, "application", "eventhandlers"), regexp.MustCompile(`(?m)^type (On\w+) struct`), 1))
	}
	return nil
}

func grep(dir string, re *regexp.Regexp, group int) []string {
	entries, _ := os.ReadDir(dir)
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			seen[m[group]] = true
		}
	}
	return sortedKeys(seen)
}

// handlersIn lists request types that have a matching <Type>Handler.
func handlersIn(dir string, typeRe *regexp.Regexp) []string {
	types := map[string]bool{}
	for _, t := range grep(dir, typeRe, 1) {
		types[t] = true
	}
	var out []string
	for t := range types {
		if types[t+"Handler"] {
			out = append(out, t)
		}
	}
	return sortedKeys(toSet(out))
}

func toSet(s []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range s {
		m[v] = true
	}
	return m
}
