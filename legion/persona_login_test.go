package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPersonaPasswordFromLocalSecrets: a persona with no password in personas.json takes it from the git-ignored
// logins.local.json, keyed by account id; a password in personas.json is kept as it is.
func TestPersonaPasswordFromLocalSecrets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "personas.json")
	if err := os.WriteFile(file, []byte(`[{"account":"player","account_id":2000000,"password":""},`+
		`{"account":"lg0001","account_id":2100001,"password":"legion"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loginSecretsFile(dir), []byte(`{"2000000":"s3cret-test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ps, err := loadPersonas(file)
	if err != nil || len(ps) != 2 {
		t.Fatalf("load: %v %d", err, len(ps))
	}
	if ps[0].Password != "s3cret-test" {
		t.Fatalf("Ayla's password not filled from the local file: %q", ps[0].Password)
	}
	if ps[1].Password != "legion" {
		t.Fatalf("a set password was changed: %q", ps[1].Password)
	}
}
