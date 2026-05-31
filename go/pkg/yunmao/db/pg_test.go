package db

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestExtractUp(t *testing.T) {
	s := `-- header
-- +goose Up
CREATE TABLE foo (id text);
-- +goose Down
DROP TABLE foo;`
	got := strings.TrimSpace(extractUp(s))
	if !strings.HasPrefix(got, "CREATE TABLE") {
		t.Fatalf("expected up section, got %q", got)
	}
	if strings.Contains(got, "DROP TABLE") {
		t.Fatalf("down section leaked: %q", got)
	}
}

func TestLoadMigrationsFSOrder(t *testing.T) {
	mfs := fstest.MapFS{
		"migrations/0002_b.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 2;")},
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;")},
		"migrations/README.md":  &fstest.MapFile{Data: []byte("# ignored")},
	}
	ms, err := LoadMigrationsFS(mfs, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(ms))
	}
	if ms[0].Name != "0001_a.sql" || ms[1].Name != "0002_b.sql" {
		t.Fatalf("wrong order: %v", ms)
	}
	if !strings.Contains(ms[0].SQL, "SELECT 1") {
		t.Fatalf("up section missing: %q", ms[0].SQL)
	}
}
