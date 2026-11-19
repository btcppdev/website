package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMigrationHookMerges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	script, err := filepath.Abs("../../githooks/check-migration-prefixes.sh")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
		return string(out)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(wantPass bool, args ...string) {
		t.Helper()
		cmd := exec.Command("sh", append([]string{script, "db/migrations"}, args...)...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantPass {
			t.Fatalf("hook pass=%v want=%v: %s", err == nil, wantPass, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "migration-test@example.test")
	git("config", "user.name", "Migration test")
	git("config", "commit.gpgsign", "false")
	git("config", "core.hooksPath", filepath.Join(repo, "no-hooks"))
	if err := os.MkdirAll(filepath.Join(repo, "db/migrations"), 0755); err != nil {
		t.Fatal(err)
	}
	write("db/migrations/001_base.sql", "SELECT 1;\n")
	git("add", ".")
	git("commit", "-m", "base")
	git("switch", "-c", "upstream")
	write("db/migrations/002_upstream.sql", "SELECT 2;\n")
	write("db/migrations/006_upstream.sql", "SELECT 6;\n")
	git("add", ".")
	git("commit", "-m", "upstream migrations")
	git("switch", "main")
	write("db/migrations/004_local.sql", "SELECT 4;\n")
	git("add", ".")
	check(true)
	git("commit", "-m", "local migration")
	git("merge", "--no-ff", "--no-commit", "upstream")
	check(true) // Both higher and lower inherited versions are legitimate.
	write("db/migrations/005_new.sql", "SELECT 5;\n")
	git("add", ".")
	check(false) // New versions must follow both parents, including upstream's 006.
	git("rm", "-f", "db/migrations/005_new.sql")
	write("db/migrations/007_new.sql", "SELECT 7;\n")
	git("add", ".")
	check(true)
	write("db/migrations/002_upstream.sql", "SELECT 'changed history';\n")
	git("add", ".")
	check(false) // Only byte-identical inherited migrations are exempt.
	git("restore", "--source=upstream", "--staged", "--worktree", "db/migrations/002_upstream.sql")
	write("db/migrations/007_duplicate.sql", "SELECT 7;\n")
	check(false)
	if err := os.Remove(filepath.Join(repo, "db/migrations/007_duplicate.sql")); err != nil {
		t.Fatal(err)
	}
	check(true)
	git("commit", "-m", "merge")
	write("db/migrations/003_late.sql", "SELECT 3;\n")
	git("add", ".")
	check(false)             // Ordinary commits still reject old version numbers.
	check(false, "upstream") // Explicit PR base comparisons keep the same check.
}
