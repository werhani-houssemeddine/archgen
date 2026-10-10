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

/* ================================================================== */
/* new                                                                 */
/* ================================================================== */

var modPathRe = regexp.MustCompile(`^[A-Za-z0-9._~\-/]+$`)

func runNew(c *ctx, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	module := fs.String("module", "", "Go module path (default: the project name)")
	kit := fs.String("kit", "github.com/werhani-houssemeddine/go-archkit", "import path of the archkit library")
	kitReplace := fs.String("kit-replace", "", "local path of archkit (adds a replace directive)")
	driver := fs.String("driver", "pgx", "database/sql driver: pgx or pq")
	migrations := fs.String("migrations", "timestamp", "migration versioning: timestamp or sequential")
	goVersion := fs.String("go", "1.24", "go directive")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("expected a project name")
	}
	name := pos[0]
	if !identRe.MatchString(name) {
		return usagef("invalid project name %q", name)
	}
	if *module == "" {
		*module = name
	}
	if !modPathRe.MatchString(*module) {
		return usagef("invalid module path %q", *module)
	}
	if *driver != "pgx" && *driver != "pq" {
		return usagef("--driver must be pgx or pq")
	}
	if *migrations != "timestamp" && *migrations != "sequential" {
		return usagef("--migrations must be timestamp or sequential")
	}
	root, err := filepath.Abs(filepath.Join(c.dir, name))
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 && !c.force {
		return fmt.Errorf("%s already exists and is not empty (use --force to generate into it)", root)
	}
	replace := *kitReplace
	if replace != "" && !filepath.IsAbs(replace) {
		// make it relative to the new project root
		absReplace, _ := filepath.Abs(replace)
		if rel, err := filepath.Rel(root, absReplace); err == nil {
			replace = rel
		}
	}

	p := &project.Project{Root: root, Config: project.Config{Module: *module, Kit: *kit, Driver: *driver, Migrations: *migrations}}
	p.Defaults()
	w := gen.New(root, c.dryRun, c.force)
	d := projectData(p)
	d.AppName = naming.Kebab(name)
	d.GoVersion = *goVersion
	d.KitReplace = replace

	files := []struct{ rel, tmpl string }{
		{"go.mod", "project_go.mod.tmpl"},
		{"cmd/api/main.go", "project_main.go.tmpl"},
		{"internal/app/bootstrap.go", "project_bootstrap.go.tmpl"},
		{"internal/app/config.go", "project_config.go.tmpl"},
		{"internal/app/dependencies.go", "project_dependencies.go.tmpl"},
		{"internal/app/router.go", "project_router.go.tmpl"},
		{"internal/app/rbac.go", "project_rbac.go.tmpl"},
		{"internal/shared/kernel/kernel.go", "project_kernel.go.tmpl"},
		{"internal/shared/database/database.go", "project_database.go.tmpl"},
		{"internal/shared/http/http.go", "project_sharedhttp.go.tmpl"},
		{"internal/shared/logging/logging.go", "project_logging.go.tmpl"},
		{"internal/shared/observability/observability.go", "project_observability.go.tmpl"},
		{"migrations/embed.go", "project_migrations_embed.go.tmpl"},
		{"Makefile", "project_Makefile.tmpl"},
		{".gitignore", "project_gitignore.tmpl"},
		{".env.example", "project_env.tmpl"},
		{"docker-compose.yml", "project_compose.yml.tmpl"},
		{"README.md", "project_README.md.tmpl"},
	}
	data := tmpl.Data{Project: d}
	for _, f := range files {
		if err := render(w, f.rel, f.tmpl, data); err != nil {
			return err
		}
	}
	if err := render(w, c.nextMigration(p, w, "init_platform"), "project_init.sql.tmpl", data); err != nil {
		return err
	}
	if !c.dryRun {
		if err := p.Save(); err != nil {
			return err
		}
	}
	w.Actions = append(w.Actions, gen.Action{Path: project.ConfigFile, Verb: "create"})
	next := []string{"cd " + filepath.Base(root)}
	if replace == "" {
		next = append(next, "go get "+*kit+"@latest")
	}
	next = append(next, "go mod tidy", "archgen module <name>   # e.g. archgen module users", "cp .env.example .env && make db-up && make run")
	c.report(w, next...)
	return nil
}

/* ================================================================== */
/* module                                                              */
/* ================================================================== */

func runModule(c *ctx, args []string) error {
	fs := flag.NewFlagSet("module", flag.ContinueOnError)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("expected a module name")
	}
	if err := validateModuleName(pos[0]); err != nil {
		return err
	}
	p, w, err := c.load()
	if err != nil {
		return err
	}
	m := tmpl.NewModule(pos[0])
	if p.HasModule(m.Pkg) && !c.force {
		return fmt.Errorf("module %q already exists", m.Pkg)
	}
	data := tmpl.Data{Project: projectData(p), Mod: m}
	base := filepath.Join(p.ModulesDir, m.Pkg)
	files := []struct{ rel, tmpl string }{
		{"module.go", "module_module.go.tmpl"},
		{"contract/contract.go", "module_contract.go.tmpl"},
		{"domain/doc.go", "module_doc_domain.go.tmpl"},
		{"application/commands/doc.go", "module_doc_commands.go.tmpl"},
		{"application/queries/doc.go", "module_doc_queries.go.tmpl"},
		{"application/ports/doc.go", "module_doc_ports.go.tmpl"},
		{"infrastructure/persistence/doc.go", "module_doc_persistence.go.tmpl"},
		{"interfaces/http/doc.go", "module_doc_http.go.tmpl"},
	}
	for _, f := range files {
		if err := render(w, filepath.Join(base, f.rel), f.tmpl, data); err != nil {
			return err
		}
	}
	if err := render(w, c.nextMigration(p, w, "create_"+m.Snake+"_schema"), "module_schema.sql.tmpl", data); err != nil {
		return err
	}

	deps := filepath.Join("internal", "app", "dependencies.go")
	if !w.Exists(deps) {
		fmt.Fprintf(c.errOut, "warning: %s not found; wire %s.New(...) manually in your composition root\n", deps, m.Pkg)
	} else {
		steps := []struct{ marker, snippet string }{
			{"module-fields", fmt.Sprintf("%s *%s.Module", m.Pascal, m.Pkg)},
			{"module-wire", fmt.Sprintf("ms.%s = %s.New(%s.Deps{DB: p.DB, Outbox: p.Outbox, Mediator: p.Mediator, Clock: p.Clock, Logger: p.Logger.With(\"module\", %q)})", m.Pascal, m.Pkg, m.Pkg, m.Snake)},
			{"module-routes", fmt.Sprintf("ms.%s.RegisterRoutes(api)", m.Pascal)},
			{"module-subscriptions", fmt.Sprintf("ms.%s.Subscribe(bus)", m.Pascal)},
		}
		for _, s := range steps {
			if err := w.Insert(deps, s.marker, s.snippet); err != nil {
				return err
			}
		}
		if err := w.EnsureImport(deps, p.ModuleImport(m.Pkg), ""); err != nil {
			return err
		}
	}
	c.report(w, fmt.Sprintf(`archgen resource %s <Aggregate> --fields "name:string, ..."`, m.Pkg))
	return nil
}

/* ================================================================== */
/* resource                                                            */
/* ================================================================== */

func runResource(c *ctx, args []string) error {
	fs := flag.NewFlagSet("resource", flag.ContinueOnError)
	spec := fs.String("fields", "", "field list, e.g. \"title:string:unique, price:float, status:enum(draft|live)\"")
	noHTTP := fs.Bool("no-http", false, "skip the HTTP adapter")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("expected <module> <Name>")
	}
	if err := validateTypeName("resource", pos[1]); err != nil {
		return err
	}
	fl, err := fields.Parse(*spec)
	if err != nil {
		return usagef("%v", err)
	}
	p, w, err := c.load()
	if err != nil {
		return err
	}
	m, err := requireModule(p, pos[0])
	if err != nil {
		return err
	}
	r := tmpl.NewResource(m, pos[1], fl)
	base := filepath.Join(p.ModulesDir, m.Pkg)
	if w.Exists(filepath.Join(base, "domain", r.Snake+".go")) && !c.force {
		return fmt.Errorf("aggregate %s already exists in module %s", r.Name, m.Pkg)
	}
	data := tmpl.Data{Project: projectData(p), Mod: m, Res: r}
	files := []struct{ rel, tmpl string }{
		{"domain/" + r.Snake + ".go", "resource_domain.go.tmpl"},
		{"domain/" + r.Snake + "_errors.go", "resource_errors.go.tmpl"},
		{"domain/" + r.Snake + "_events.go", "resource_events.go.tmpl"},
		{"domain/" + r.Snake + "_repository.go", "resource_repository.go.tmpl"},
		{"domain/" + r.Snake + "_test.go", "resource_domain_test.go.tmpl"},
		{"application/commands/create_" + r.Snake + ".go", "resource_cmd_create.go.tmpl"},
		{"application/commands/update_" + r.Snake + ".go", "resource_cmd_update.go.tmpl"},
		{"application/commands/delete_" + r.Snake + ".go", "resource_cmd_delete.go.tmpl"},
		{"application/commands/" + r.Snake + "_handlers.go", "resource_cmd_register.go.tmpl"},
		{"application/commands/" + r.Snake + "_commands_test.go", "resource_cmd_test.go.tmpl"},
		{"application/queries/" + r.Snake + "_view.go", "resource_view.go.tmpl"},
		{"application/queries/get_" + r.Snake + ".go", "resource_query_get.go.tmpl"},
		{"application/queries/list_" + r.PluralSnake + ".go", "resource_query_list.go.tmpl"},
		{"application/queries/" + r.Snake + "_handlers.go", "resource_query_register.go.tmpl"},
		{"infrastructure/persistence/postgres_" + r.Snake + "_repository.go", "resource_pg_repo.go.tmpl"},
		{"infrastructure/persistence/memory_" + r.Snake + "_repository.go", "resource_mem_repo.go.tmpl"},
	}
	if !*noHTTP {
		files = append(files,
			struct{ rel, tmpl string }{"interfaces/http/" + r.Snake + "_handler.go", "resource_http.go.tmpl"},
			struct{ rel, tmpl string }{"interfaces/http/" + r.Snake + "_handler_test.go", "resource_http_test.go.tmpl"},
		)
	}
	for _, f := range files {
		if err := render(w, filepath.Join(base, f.rel), f.tmpl, data); err != nil {
			return err
		}
	}
	if err := render(w, c.nextMigration(p, w, "create_"+m.Snake+"_"+r.PluralSnake), "resource_table.sql.tmpl", data); err != nil {
		return err
	}

	mod := filepath.Join(base, "module.go")
	repoField := r.Camel + "Repo"
	steps := []struct{ marker, snippet string }{
		{"fields", fmt.Sprintf("%s domain.%sRepository", repoField, r.Name)},
		{"wire", fmt.Sprintf("m.%s = persistence.NewPostgres%sRepository(d.DB, d.Outbox)\ncommands.Register%sHandlers(d.Mediator, m.%s, d.Clock)\nqueries.Register%sHandlers(d.Mediator, m.%s)", repoField, r.Name, r.Name, repoField, r.Name, repoField)},
	}
	if !*noHTTP {
		steps = append(steps, struct{ marker, snippet string }{"routes", fmt.Sprintf("modhttp.Register%sRoutes(g, m.deps.Mediator)", r.Name)})
	}
	for _, s := range steps {
		if err := w.Insert(mod, s.marker, s.snippet); err != nil {
			return err
		}
	}
	imports := [][2]string{
		{p.ModuleImport(m.Pkg, "domain"), ""},
		{p.ModuleImport(m.Pkg, "infrastructure", "persistence"), ""},
		{p.ModuleImport(m.Pkg, "application", "commands"), ""},
		{p.ModuleImport(m.Pkg, "application", "queries"), ""},
	}
	if !*noHTTP {
		imports = append(imports, [2]string{p.ModuleImport(m.Pkg, "interfaces", "http"), "modhttp"})
	}
	for _, imp := range imports {
		if err := w.EnsureImport(mod, imp[0], imp[1]); err != nil {
			return err
		}
	}
	perm := r.Perm
	c.report(w,
		"go test ./"+filepath.ToSlash(base)+"/...",
		fmt.Sprintf("grant permissions %s:read|create|update|delete via roles in internal/app/rbac.go", perm),
		"add business methods to domain/"+r.Snake+".go (e.g. Publish, Archive) and expose them with `archgen command`",
	)
	return nil
}

/* ================================================================== */
/* migration                                                           */
/* ================================================================== */

func runMigration(c *ctx, args []string) error {
	fs := flag.NewFlagSet("migration", flag.ContinueOnError)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("expected <module|-> <name>")
	}
	p, w, err := c.load()
	if err != nil {
		return err
	}
	header := "-- Platform migration (no module).\n"
	prefix := ""
	if pos[0] != "-" {
		m, err := requireModule(p, pos[0])
		if err != nil {
			return err
		}
		header = fmt.Sprintf("-- Migration of the %s module. Only touch objects in the %q schema.\n", m.Human, m.Snake)
		prefix = m.Snake + "_"
	}
	name := naming.Snake(pos[1])
	if name == "" {
		return usagef("invalid migration name")
	}
	rel := c.nextMigration(p, w, prefix+name)
	body := header + "-- Migrations are forward-only and run inside a transaction; never edit an applied file.\n\n"
	if err := w.Write(rel, []byte(body)); err != nil {
		return err
	}
	c.report(w, "edit "+filepath.ToSlash(rel))
	return nil
}

// moduleBase returns the relative directory of a module.
func moduleBase(p *project.Project, m tmpl.Module) string { return filepath.Join(p.ModulesDir, m.Pkg) }

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}
