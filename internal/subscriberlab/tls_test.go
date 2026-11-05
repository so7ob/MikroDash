package subscriberlab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestPublicLabCertificateRejectsPrivateOrUnexpectedMaterial(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := validatePublicLabCertificate("md-lab-ca", public); err != nil {
		t.Fatal(err)
	}
	secret, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: secret})
	for name, raw := range map[string][]byte{"private": private, "combined": append(append([]byte{}, public...), private...), "duplicate": append(append([]byte{}, public...), public...), "prefix": append([]byte("unexpected\n"), public...), "header": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"Unexpected": "data"}, Bytes: der}), "invalid": []byte("unknown")} {
		t.Run(name, func(t *testing.T) {
			if validatePublicLabCertificate("md-lab-ca", raw) == nil {
				t.Fatal("non-public or ambiguous export accepted")
			}
		})
	}
	if validatePublicLabCertificate("md-lab-server", public) == nil || validatePublicLabCertificate("operator", public) == nil {
		t.Fatal("unexpected certificate role accepted")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"portal.example.test", "login.example.test"},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePublicLabCertificate("md-lab-server", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		t.Fatal(err)
	}
	leaf.DNSNames = []string{"other.example.test"}
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if validatePublicLabCertificate("md-lab-server", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) == nil {
		t.Fatal("wrong server identity accepted")
	}
}

func TestLabBootstrapBooleanReadback(t *testing.T) {
	for _, pair := range [][2]string{{"true", "yes"}, {"false", "no"}, {"yes", "yes"}, {"no", "no"}, {"unchanged", "unchanged"}} {
		if !sameLabValue(pair[0], pair[1]) {
			t.Fatal("equivalent API readback refused", pair)
		}
	}
	for _, pair := range [][2]string{{"false", "yes"}, {"true", "no"}, {"", "no"}, {"different", "unchanged"}} {
		if sameLabValue(pair[0], pair[1]) {
			t.Fatal("changed or unknown fixture accepted", pair)
		}
	}
}
