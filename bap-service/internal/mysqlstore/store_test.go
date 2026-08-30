package mysqlstore

import (
	"strings"
	"testing"
)

func TestSecuredDSNRequiresTLSOutsideDevelopment(t *testing.T) {
	_, err := securedDSN(Config{DSN: "bap:secret@tcp(mysql.example:3306)/bap"})
	if err == nil || !strings.Contains(err.Error(), "TLS is required") {
		t.Fatalf("unencrypted network DSN error = %v, want TLS requirement", err)
	}
}

func TestSecuredDSNAppliesSafeDefaultsForLocalDevelopment(t *testing.T) {
	dsn, err := securedDSN(Config{DSN: "bap:secret@tcp(mysql:3306)/bap", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"parseTime=true", "timeout=3s", "readTimeout=5s", "writeTimeout=5s"} {
		if !strings.Contains(dsn, required) {
			t.Fatalf("secured DSN %q is missing %q", dsn, required)
		}
	}
}

func TestSecuredDSNRejectsUnverifiedTLS(t *testing.T) {
	_, err := securedDSN(Config{DSN: "bap:secret@tcp(mysql.example:3306)/bap?tls=skip-verify"})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("unverified TLS DSN error = %v, want forbidden", err)
	}
}
