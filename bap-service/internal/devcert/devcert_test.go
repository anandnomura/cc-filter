package devcert

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

func TestEnsureCertificateSupportsNativeAndContainerLocalNames(t *testing.T) {
	certPath, _, _, err := Ensure(t.TempDir())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("certificate PEM could not be decoded")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	for _, hostname := range []string{"localhost", "127.0.0.1", "host.containers.internal"} {
		if err := certificate.VerifyHostname(hostname); err != nil {
			t.Errorf("certificate does not support %q: %v", hostname, err)
		}
	}
}
