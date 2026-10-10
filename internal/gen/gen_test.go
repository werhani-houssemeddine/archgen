package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const src = `package demo

import "fmt"

type Module struct {
	// archgen:fields
}

func New() {
	fmt.Println("x")
	// archgen:wire
}
`

func TestInsertIsIdempotentAndKeepsIndentation(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o644)
	w := New(dir, false, false)
	w.LocalPrefix = "example.com/app"
	for i := 0; i < 2; i++ {
		if err := w.Insert("m.go", "wire", "a := 1\n_ = a"); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.EnsureImport("m.go", "example.com/app/internal/x", "xx"); err != nil {
		t.Fatal(err)
	}
	if err := w.EnsureImport("m.go", "strings", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "m.go"))
	s := string(b)
	if strings.Count(s, "a := 1") != 1 || !strings.Contains(s, "\ta := 1\n\t_ = a\n\t// archgen:wire") {
		t.Fatalf("insert:\n%s", s)
	}
	if !strings.Contains(s, "import (\n\t\"fmt\"\n\t\"strings\"\n\n\txx \"example.com/app/internal/x\"\n)") {
		t.Fatalf("imports not grouped:\n%s", s)
	}
	if err := w.Insert("m.go", "missing", "x"); err == nil {
		t.Fatal("missing marker must fail")
	}
}

func TestWriteSkipsExistingAndRejectsInvalidGo(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, false, false)
	if err := w.Write("a.go", []byte("package a\nimport \"fmt\"\nfunc A() {}\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if strings.Contains(string(b), "fmt") {
		t.Fatal("unused imports must be pruned")
	}
	_ = w.Write("a.go", []byte("package a\n"))
	if w.Actions[len(w.Actions)-1].Verb != "skip" {
		t.Fatal("existing files must be skipped without --force")
	}
	if err := w.Write("b.go", []byte("package b\nfunc {")); err == nil {
		t.Fatal("invalid Go must be reported")
	}
	dry := New(t.TempDir(), true, false)
	_ = dry.Write("c.go", []byte("package c\n"))
	if _, err := os.Stat(filepath.Join(dry.Root, "c.go")); err == nil {
		t.Fatal("dry run must not write")
	}
}
