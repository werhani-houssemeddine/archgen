package cli

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/werhani-houssemeddine/archgen/internal/check"
	"github.com/werhani-houssemeddine/archgen/internal/project"
)

func runCheck(c *ctx, args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	tests := fs.Bool("tests", false, "also check _test.go files")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := c.parse(fs, args); err != nil {
		return err
	}
	p, err := project.Load(c.dir)
	if err != nil {
		return err
	}
	vs, err := check.Run(check.Options{Root: p.Root, Module: p.Module, ModulesDir: p.ModulesDir, Kit: p.Kit, IncludeTests: *tests})
	if err != nil {
		return err
	}
	if *asJSON {
		if vs == nil {
			vs = []check.Violation{}
		}
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(vs)
	} else if len(vs) == 0 {
		mods, _ := p.Modules()
		fmt.Fprintf(c.out, "✓ architecture rules respected (%d modules)\n", len(mods))
	} else {
		for _, v := range vs {
			fmt.Fprintln(c.errOut, v.String())
		}
		fmt.Fprintf(c.errOut, "\n✗ %d architecture violation(s)\n", len(vs))
	}
	if len(vs) > 0 {
		return checkFailed{}
	}
	return nil
}
