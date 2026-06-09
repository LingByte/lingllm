// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"math/big"
	"strings"
)

// OID for id-pe-TNAuthList per RFC 8226 §9.
var oidTNAuthList = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 26}

// TNAuthList is a parsed TN Authorization List extension.
type TNAuthList struct {
	SPCs   []string
	Ranges []TNRange
	Ones   []string
}

// TNRange is one RFC 8226 TelephoneNumberRange entry.
type TNRange struct {
	Start string
	Count int
}

type tnRangeASN struct {
	Start string
	Count int
}

// AuthorizesTN reports whether origTN is permitted by the TNAuthList.
func ParseTNAuthListExtension(raw []byte) (TNAuthList, error) {
	var list TNAuthList
	if len(raw) == 0 {
		return list, nil
	}
	var seq asn1.RawValue
	if _, err := asn1.Unmarshal(raw, &seq); err != nil {
		return list, fmt.Errorf("stir: tn auth list: %w", err)
	}
	rest := seq.Bytes
	for len(rest) > 0 {
		var entry asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &entry)
		if err != nil {
			return list, fmt.Errorf("stir: tn auth list entry: %w", err)
		}
		if entry.Class != asn1.ClassContextSpecific {
			continue
		}
		switch entry.Tag {
		case 0:
			var spc string
			if _, err := asn1.Unmarshal(entry.FullBytes, &spc); err == nil && spc != "" {
				list.SPCs = append(list.SPCs, spc)
			}
		case 1:
			var r tnRangeASN
			if _, err := asn1.Unmarshal(entry.Bytes, &r); err == nil && r.Start != "" && r.Count >= 2 {
				list.Ranges = append(list.Ranges, TNRange{Start: r.Start, Count: r.Count})
			}
		case 2:
			var one string
			if _, err := asn1.Unmarshal(entry.Bytes, &one); err == nil && one != "" {
				list.Ones = append(list.Ones, one)
			} else if _, err := asn1.Unmarshal(entry.FullBytes, &one); err == nil && one != "" {
				list.Ones = append(list.Ones, one)
			}
		}
	}
	return list, nil
}

// TNAuthListFromCert extracts the TN Authorization List from a leaf cert.
func TNAuthListFromCert(cert *x509.Certificate) (TNAuthList, bool) {
	if cert == nil {
		return TNAuthList{}, false
	}
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidTNAuthList) {
			list, err := ParseTNAuthListExtension(ext.Value)
			if err != nil {
				return TNAuthList{}, true
			}
			return list, true
		}
	}
	return TNAuthList{}, false
}

// AuthorizesTN reports whether origTN is permitted by the TNAuthList.
// When the extension is absent, returns (true, false).
// When present with TN entries, origTN must match a one/range entry.
// SPC-only lists do not constrain individual TNs (carrier scope).
func (l TNAuthList) AuthorizesTN(origTN string) (ok bool, constrained bool) {
	tn := tnAuthDigits(origTN)
	if tn == "" {
		return false, len(l.Ones) > 0 || len(l.Ranges) > 0
	}
	if len(l.Ones) == 0 && len(l.Ranges) == 0 {
		return true, false
	}
	for _, one := range l.Ones {
		if tn == tnAuthDigits(one) {
			return true, true
		}
	}
	for _, r := range l.Ranges {
		if tnInRange(tn, r) {
			return true, true
		}
	}
	return false, true
}

func tnAuthDigits(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if c := CanonicalE164(s); c != "" {
		return strings.TrimPrefix(c, "+")
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func tnInRange(tn string, r TNRange) bool {
	start := tnAuthDigits(r.Start)
	if start == "" || r.Count < 2 || tn == "" {
		return false
	}
	if len(tn) != len(start) {
		return false
	}
	startInt, ok1 := new(big.Int).SetString(start, 10)
	tnInt, ok2 := new(big.Int).SetString(tn, 10)
	if !ok1 || !ok2 {
		return false
	}
	end := new(big.Int).Add(startInt, big.NewInt(int64(r.Count-1)))
	return tnInt.Cmp(startInt) >= 0 && tnInt.Cmp(end) <= 0
}

// CertAuthorizesOrigTN checks the leaf cert TNAuthList against PASSporT orig.tn.
func CertAuthorizesOrigTN(cert *x509.Certificate, origTN string) error {
	list, present := TNAuthListFromCert(cert)
	if !present {
		return nil
	}
	ok, constrained := list.AuthorizesTN(origTN)
	if !constrained {
		return nil
	}
	if !ok {
		return fmt.Errorf("orig.tn %q not in TN Authorization List", origTN)
	}
	return nil
}
