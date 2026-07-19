package getters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Getter queries run on HTTP request paths. An unbounded background context
// here can hold a pool connection and request goroutine forever when Postgres
// stalls, so keep the timeout boundary enforceable as the data layer evolves.
func TestProductionGettersDoNotUseBackgroundDatabaseContexts(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read getters directory: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(contents), "context.Background()") {
			t.Errorf("%s uses context.Background(); database work in getters must use the bounded DatabaseContext", name)
		}
	}
}
