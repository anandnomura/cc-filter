package grants

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const Type = "BAP-Grant-EdDSA"

type Claims struct {
	Issuer                string `json:"iss"`
	Audience              string `json:"aud"`
	Subject               string `json:"sub"`
	Action                string `json:"action"`
	Resource              string `json:"resource"`
	SessionID             string `json:"session_id"`
	RequestHash           string `json:"request_hash"`
	DecisionID            string `json:"decision_id"`
	Principal             string `json:"principal,omitempty"`
	CredentialFingerprint string `json:"credential_fingerprint,omitempty"`
	PolicyVersion         string `json:"policy_version,omitempty"`
	IssuedAt              int64  `json:"iat"`
	ExpiresAt             int64  `json:"exp"`
}

type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func GenerateKeyPair(privatePath, publicPath string) error {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600); err != nil {
		return err
	}
	return os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0644)
}

func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not Ed25519")
	}
	return privateKey, nil
}

func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return publicKey, nil
}

func Sign(privateKey ed25519.PrivateKey, claims Claims) (string, error) {
	headerJSON, _ := json.Marshal(header{Algorithm: "EdDSA", Type: Type})
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := encode(headerJSON) + "." + encode(claimsJSON)
	signature, err := privateKey.Sign(rand.Reader, []byte(unsigned), crypto.Hash(0))
	if err != nil {
		return "", err
	}
	return unsigned + "." + encode(signature), nil
}

func Verify(publicKey ed25519.PublicKey, token, audience, requestHash string, now time.Time) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("grant must contain three segments")
	}
	headerJSON, err := decode(parts[0])
	if err != nil {
		return claims, fmt.Errorf("decode grant header: %w", err)
	}
	var h header
	if err := json.Unmarshal(headerJSON, &h); err != nil || h.Algorithm != "EdDSA" || h.Type != Type {
		return claims, errors.New("unsupported grant header")
	}
	signature, err := decode(parts[2])
	if err != nil || !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return claims, errors.New("invalid grant signature")
	}
	claimsJSON, err := decode(parts[1])
	if err != nil || json.Unmarshal(claimsJSON, &claims) != nil {
		return claims, errors.New("invalid grant claims")
	}
	if claims.Audience != audience {
		return claims, errors.New("grant audience mismatch")
	}
	if claims.RequestHash != requestHash {
		return claims, errors.New("grant is not bound to this request")
	}
	if claims.ExpiresAt <= now.Unix() || claims.IssuedAt > now.Add(30*time.Second).Unix() {
		return claims, errors.New("grant is expired or not yet valid")
	}
	return claims, nil
}

func HashRequest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func encode(value []byte) string          { return base64.RawURLEncoding.EncodeToString(value) }
func decode(value string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(value) }
