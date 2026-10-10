package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(append(args, "--dir", dir), &out, &errOut)
	return out.String() + errOut.String(), code
}

// TestScaffoldAndCheck generates a multi-module project, then (when ARCHKIT_PATH points
// to a local archkit checkout) compiles and tests the generated code.
func TestScaffoldAndCheck(t *testing.T) {
	tmp := t.TempDir()
	kit := os.Getenv("ARCHKIT_PATH")
	args := []string{"new", "backend", "--module", "github.com/acme/backend", "--driver", "pq"}
	if kit != "" {
		args = append(args, "--kit-replace", kit)
	}
	if out, code := run(t, tmp, args...); code != 0 {
		t.Fatalf("new: %s", out)
	}
	root := filepath.Join(tmp, "backend")
	steps := [][]string{
		{"module", "users"}, {"module", "courses"}, {"module", "enrollments"},
		{"resource", "users", "User", "--fields", "email:email:unique, fullName:string"},
		{"resource", "courses", "Course", "--fields", "title:string:unique, level:enum(beginner|advanced), price:float, startsAt:time:optional"},
		{"resource", "enrollments", "Enrollment", "--fields", "userId:uuid, courseId:uuid"},
		{"contract", "users", "UserReader", "--resource", "User"},
		{"event", "users", "UserRegistered", "--fields", "userId:uuid", "--public"},
		{"subscribe", "enrollments", "users.UserRegistered"},
		{"use", "enrollments", "users.UserReader"},
		{"command", "courses", "PublishCourse", "--fields", "courseId:uuid"},
		{"query", "courses", "CountCourses"},
		{"port", "users", "PasswordHasher", "--adapter", "security"},
		{"event", "courses", "CoursePublished", "--fields", "courseId:uuid"},
		{"migration", "courses", "add_tags"},
	}
	for _, s := range steps {
		if out, code := run(t, root, s...); code != 0 {
			t.Fatalf("%v: %s", s, out)
		}
	}
	// idempotency & guards
	if out, code := run(t, root, "module", "users"); code == 0 {
		t.Fatalf("duplicate module must fail: %s", out)
	}
	if out, code := run(t, root, "resource", "nope", "X"); code == 0 || !strings.Contains(out, "does not exist") {
		t.Fatalf("unknown module: %s", out)
	}
	if out, code := run(t, root, "module", "http"); code != 2 {
		t.Fatalf("reserved module name: %d %s", code, out)
	}
	if out, code := run(t, root, "check"); code != 0 {
		t.Fatalf("check: %s", out)
	}
	if out, _ := run(t, root, "list"); !strings.Contains(out, "UserReader") || !strings.Contains(out, "OnUsersUserRegistered") {
		t.Fatalf("list: %s", out)
	}
	// a boundary violation must be caught
	bad := filepath.Join(root, "internal/modules/enrollments/application/commands/bad.go")
	_ = os.WriteFile(bad, []byte("package commands\n\nimport _ \"github.com/acme/backend/internal/modules/users/domain\"\n"), 0o644)
	if out, code := run(t, root, "check"); code != 1 || !strings.Contains(out, "module-boundary") {
		t.Fatalf("violation not detected: %d %s", code, out)
	}
	_ = os.Remove(bad)

	if kit == "" {
		t.Log("ARCHKIT_PATH not set: skipping compilation of the generated project")
		return
	}
	for _, cmd := range [][]string{{"go", "mod", "tidy"}, {"go", "vet", "./..."}, {"go", "test", "./..."}} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = root
		c.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", cmd, err, out)
		}
	}
}

func TestUsage(t *testing.T) {
	var out bytes.Buffer
	if Run(nil, &out, &out) != 0 || !strings.Contains(out.String(), "resource") {
		t.Fatal(out.String())
	}
	out.Reset()
	if Run([]string{"bogus"}, &out, &out) != 2 {
		t.Fatal("unknown command must exit 2")
	}
	out.Reset()
	if Run([]string{"resource", "x"}, &out, &out) != 2 || !strings.Contains(out.String(), "Usage") {
		t.Fatal(out.String())
	}
}
