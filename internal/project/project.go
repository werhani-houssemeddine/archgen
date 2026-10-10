// Package project locates the target project and its archgen configuration.
package project

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigFile is stored at the project root.
const ConfigFile = ".archgen.json"

// Config describes how code is generated for a project.
type Config struct {
	// Module is the Go module path of the project (from go.mod).
	Module string `json:"module"`
	// Kit is the import path of the archkit library.
	Kit string `json:"kit"`
	// Migrations is "timestamp" (default, merge-friendly) or "sequential".
	Migrations string `json:"migrations"`
	// MigrationsDir relative to the root (default "migrations").
	MigrationsDir string `json:"migrationsDir"`
	// ModulesDir relative to the root (default "internal/modules").
	ModulesDir string `json:"modulesDir"`
	// APIPrefix for HTTP routes (default "/api/v1").
	APIPrefix string `json:"apiPrefix"`
	// Driver is the database/sql driver: "pgx" or "pq".
	Driver string `json:"driver"`
}

// Project is a loaded project.
type Project struct {
	Root string
	Config
}

// Defaults fills empty settings.
func (c *Config) Defaults() {
	if c.Kit == "" {
		c.Kit = "github.com/werhani-houssemeddine/go-archkit"
	}
	if c.Migrations == "" {
		c.Migrations = "timestamp"
	}
	if c.MigrationsDir == "" {
		c.MigrationsDir = "migrations"
	}
	if c.ModulesDir == "" {
		c.ModulesDir = "internal/modules"
	}
	if c.APIPrefix == "" {
		c.APIPrefix = "/api/v1"
	}
	if c.Driver == "" {
		c.Driver = "pgx"
	}
}

// Load finds the project root (walking up from dir to the nearest go.mod) and reads
// .archgen.json (optional; values default from go.mod).
func Load(dir string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := abs
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, fmt.Errorf("no go.mod found in %s or its parents (run `archgen new` first)", abs)
		}
		root = parent
	}
	p := &Project{Root: root}
	if b, err := os.ReadFile(filepath.Join(root, ConfigFile)); err == nil {
		if err := json.Unmarshal(b, &p.Config); err != nil {
			return nil, fmt.Errorf("%s: %w", ConfigFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if p.Module == "" {
		mod, err := ModulePath(filepath.Join(root, "go.mod"))
		if err != nil {
			return nil, err
		}
		p.Module = mod
	}
	p.Defaults()
	return p, nil
}

// Save writes .archgen.json.
func (p *Project) Save() error {
	b, _ := json.MarshalIndent(p.Config, "", "  ")
	return os.WriteFile(filepath.Join(p.Root, ConfigFile), append(b, '\n'), 0o644)
}

// ModulePath reads the module directive of a go.mod file.
func ModulePath(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), `"`), nil
		}
	}
	return "", fmt.Errorf("%s: no module directive", gomod)
}

// ModuleDir returns the absolute directory of a business module.
func (p *Project) ModuleDir(pkg string) string { return filepath.Join(p.Root, p.ModulesDir, pkg) }

// ModuleImport returns the import path of a business module package.
func (p *Project) ModuleImport(pkg string, sub ...string) string {
	parts := append([]string{p.Module, filepath.ToSlash(p.ModulesDir), pkg}, sub...)
	return strings.Join(parts, "/")
}

// HasModule reports whether the module exists.
func (p *Project) HasModule(pkg string) bool {
	_, err := os.Stat(filepath.Join(p.ModuleDir(pkg), "module.go"))
	return err == nil
}

// Modules lists existing business modules.
func (p *Project) Modules() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(p.Root, p.ModulesDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && p.HasModule(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
