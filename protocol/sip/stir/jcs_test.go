// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"testing"
)

func TestCanonicalJSON_KeyOrder(t *testing.T) {
	hdr := PassportHeader{Alg: AlgES256, Typ: "passport", X5u: "https://x.example/c.pem", Ppt: PptShaken}
	b, err := canonicalJSON(hdr)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if got[0] != '{' || got[len(got)-1] != '}' {
		t.Fatalf("not object: %q", got)
	}
	// Keys must be sorted: alg before ppt before typ before x5u
	if want := `"alg":"ES256","ppt":"shaken","typ":"passport","x5u":"https://x.example/c.pem"`; got != `{`+want+`}` {
		t.Fatalf("unexpected order: %s", got)
	}
}

func TestSignPassport_UsesJCS(t *testing.T) {
	key := newES256Key(t)
	hdr, claims := validPassport()
	signed, err := SignPassport(hdr, claims, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPassport(signed.Compact, &key.PublicKey); err != nil {
		t.Fatalf("verify after JCS sign: %v", err)
	}
}
