// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

// PEM helpers for loading the ES256 (P-256) signing key + public
// cert chain a service provider was issued by an STI-CA. These are
// thin wrappers over crypto/x509 — they exist mainly to enforce the
// RFC 8588 §5 requirement that the key be P-256 ECDSA before the
// rest of the pipeline accepts it.
//
// Cert chain fetching from the `x5u` URL belongs in a separate
// layer (network + caching policy out of scope for this package).
// Once fetched, the caller passes a *x509.Certificate.PublicKey
// (which is *ecdsa.PublicKey) into VerifyPassport.

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// LoadES256PrivateKey parses a PEM-encoded ECDSA P-256 private key.
// Accepts both the SEC1 ("EC PRIVATE KEY") and PKCS#8 ("PRIVATE
// KEY") block types — STI-CA issuance pipelines emit one or the
// other depending on tooling. Returns an error on:
//
//   * any other PEM block type
//   * non-EC keys (RSA / Ed25519 / etc.)
//   * EC keys whose curve isn't P-256
//
// Use LoadES256PrivateKeyFile to read straight from disk.
func LoadES256PrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("stir: no PEM block found")
	}
	var key any
	var err error
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("stir: unexpected PEM block type %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("stir: parse private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("stir: key is %T, need *ecdsa.PrivateKey", key)
	}
	if ecKey.Curve == nil || ecKey.Curve.Params().BitSize != 256 {
		return nil, errors.New("stir: private key curve must be P-256")
	}
	return ecKey, nil
}

// LoadES256PrivateKeyFile is LoadES256PrivateKey + os.ReadFile.
func LoadES256PrivateKeyFile(path string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("stir: read %s: %w", path, err)
	}
	return LoadES256PrivateKey(b)
}

// LoadES256Certificate parses a PEM "CERTIFICATE" block and returns
// the leaf cert. Validates that the embedded public key is ECDSA
// P-256 so signing-key/cert mismatches are caught at load time.
func LoadES256Certificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("stir: no PEM block found")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("stir: expected CERTIFICATE block, got %q", block.Type)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("stir: parse cert: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("stir: cert public key is %T, need ECDSA P-256", cert.PublicKey)
	}
	if pub.Curve == nil || pub.Curve.Params().BitSize != 256 {
		return nil, errors.New("stir: cert public key curve must be P-256")
	}
	return cert, nil
}

// PublicKeyFromCert is a tiny convenience: cert → *ecdsa.PublicKey
// suitable for VerifyPassport. Panics on non-ECDSA certs (caller
// must have already gone through LoadES256Certificate).
func PublicKeyFromCert(cert *x509.Certificate) *ecdsa.PublicKey {
	return cert.PublicKey.(*ecdsa.PublicKey)
}
