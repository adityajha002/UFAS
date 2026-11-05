package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS random source failing is not recoverable
	}
	return b
}

// randHex returns 2n hex characters (n random bytes).
func randHex(n int) string { return hex.EncodeToString(randBytes(n)) }

// randNonce returns 32 random bytes, base64url without padding.
func randNonce() string { return base64.RawURLEncoding.EncodeToString(randBytes(32)) }

func hashCode(code string) []byte {
	h := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(code))))
	return h[:]
}

// The phone's Kotlin module builds these exact strings; they must match byte for byte.
func attendMsg(challengeID, nonce, facultyID string, latMicro, lonMicro, accuracyCm int64) string {
	return fmt.Sprintf("attend.v1|%s|%s|%s|%d|%d|%d", challengeID, nonce, facultyID, latMicro, lonMicro, accuracyCm)
}

func historyMsg(challengeID, nonce, facultyID string) string {
	return fmt.Sprintf("history.v1|%s|%s|%s", challengeID, nonce, facultyID)
}

func asP256(key any) (*ecdsa.PublicKey, error) {
	pub, ok := key.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("not an ECDSA P-256 public key")
	}
	return pub, nil
}

// parsePublicKey accepts base64 SubjectPublicKeyInfo DER and returns the canonical DER.
func parsePublicKey(b64 string) ([]byte, error) {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, err := asP256(key)
	if err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(pub)
}

// verifySig checks a base64 DER ECDSA signature over SHA-256(msg).
func verifySig(pubDER []byte, msg, sigB64 string) bool {
	if len(pubDER) == 0 {
		return false
	}
	key, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return false
	}
	pub, err := asP256(key)
	if err != nil {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	h := sha256.Sum256([]byte(msg))
	return ecdsa.VerifyASN1(pub, h[:], sig)
}
