// Offline tests for scripts/db/psql-yq: the real tool, driven with committed
// fixture-shaped profiles and a fake `docker` stub — no network, no real
// Postgres. Proves the onlyoffice profile resolves (host/port/user/db), the
// password is sourced from password_env_file, and the read-only guard rejects
// DML before any client is invoked.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/pkg/repo"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func psqlYq(t *testing.T) string {
	t.Helper()
	script := filepath.Join(repo.Root(), "scripts", "db", "psql-yq")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("psql-yq not found: %v", err)
	}
	return script
}

func testEnv(dir string) []string {
	return append(os.Environ(),
		"BRAIN_DB_PROFILES="+filepath.Join(dir, "db-profiles.yml"),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
}

// TestPsqlYqOnlyOfficeProfile builds a fixture profile with a synthetic
// onlyoffice connection (127.0.0.1:5433, user/db onlyoffice) and runs the real
// psql-yq against a fake docker stub, asserting the docker invocation carries
// the resolved host/port/user/db and PGPASSWORD from the env file.
func TestPsqlYqOnlyOfficeProfile(t *testing.T) {
	if _, err := exec.LookPath("yq"); err != nil {
		t.Skip("yq not installed; psql-yq requires it")
	}
	dir := t.TempDir()
	envFile := filepath.Join(dir, "onlyoffice.env")
	writeFile(t, envFile, "PGPASSWORD=supersecret\n")
	writeFile(t, filepath.Join(dir, "db-profiles.yml"), `onlyoffice:
  host: 127.0.0.1
  port: 5433
  user: onlyoffice
  db: onlyoffice
  password_env_file: `+envFile+`
  network: host
`)

	log := filepath.Join(dir, "docker.log")
	writeFile(t, filepath.Join(dir, "docker"), `#!/bin/sh
echo "$@" >> "`+log+`"
echo "PGPASSWORD=$PGPASSWORD" >> "`+log+`"
exit 0
`)
	if err := os.Chmod(filepath.Join(dir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(psqlYq(t), "--profile", "onlyoffice", "-s", "document_asset")
	cmd.Env = testEnv(dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("psql-yq failed: %v\n%s", err, out)
	}

	logB, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	s := string(logB)
	for _, want := range []string{"--network host", "-h 127.0.0.1", "-p 5433", "-U onlyoffice", "-d onlyoffice"} {
		if !strings.Contains(s, want) {
			t.Errorf("docker stub invocation missing %q\n%s", want, s)
		}
	}
	if !strings.Contains(s, "PGPASSWORD=supersecret") {
		t.Errorf("password not sourced from password_env_file\n%s", s)
	}
}

// TestPsqlYqRejectsWrite enforces the read-only guard offline: a DML query is
// rejected (exit 3) before any docker client would run.
func TestPsqlYqRejectsWrite(t *testing.T) {
	if _, err := exec.LookPath("yq"); err != nil {
		t.Skip("yq not installed; psql-yq requires it")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "onlyoffice.env"), "PGPASSWORD=x\n")
	writeFile(t, filepath.Join(dir, "db-profiles.yml"), `onlyoffice:
  host: 127.0.0.1
  port: 5433
  user: onlyoffice
  db: onlyoffice
  password_env_file: `+filepath.Join(dir, "onlyoffice.env")+`
`)
	// Fake docker that fails loudly if the guard lets a write reach the client.
	writeFile(t, filepath.Join(dir, "docker"), "#!/bin/sh\necho 'docker should not run' >&2\nexit 99\n")
	if err := os.Chmod(filepath.Join(dir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(psqlYq(t), "--profile", "onlyoffice", "-c", "DELETE FROM document_asset")
	cmd.Env = testEnv(dir)
	err := cmd.Run()
	if err == nil {
		t.Fatal("write query accepted; read-only guard must reject it")
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("exit = %v, want 3 (read-only rejection)", err)
	}
}
