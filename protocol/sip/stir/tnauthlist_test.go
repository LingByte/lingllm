// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package stir

import (
	"encoding/asn1"
	"testing"
)

func TestTNAuthList_OneAndRange(t *testing.T) {
	list := TNAuthList{
		Ones:   []string{"15551234567"},
		Ranges: []TNRange{{Start: "15559999000", Count: 10}},
	}
	if ok, _ := list.AuthorizesTN("+15551234567"); !ok {
		t.Fatal("one match")
	}
	if ok, _ := list.AuthorizesTN("+15559999005"); !ok {
		t.Fatal("range match")
	}
	if ok, constrained := list.AuthorizesTN("+15550001111"); ok || !constrained {
		t.Fatal("out of range should fail")
	}
}

func TestParseTNAuthListExtension_RoundTrip(t *testing.T) {
	oneInner, err := asn1.Marshal("15551234567")
	if err != nil {
		t.Fatal(err)
	}
	oneTagged, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 2, Bytes: oneInner})
	if err != nil {
		t.Fatal(err)
	}
	rangeInner, err := asn1.Marshal(tnRangeASN{Start: "15559999000", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	rangeTagged, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: rangeInner})
	if err != nil {
		t.Fatal(err)
	}
	var oneRV, rangeRV asn1.RawValue
	if _, err := asn1.Unmarshal(oneTagged, &oneRV); err != nil {
		t.Fatal(err)
	}
	if _, err := asn1.Unmarshal(rangeTagged, &rangeRV); err != nil {
		t.Fatal(err)
	}
	listDER, err := asn1.Marshal([]asn1.RawValue{oneRV, rangeRV})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseTNAuthListExtension(listDER)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := list.AuthorizesTN("+15551234567"); !ok {
		t.Fatalf("parsed list: %+v", list)
	}
}

func TestTNAuthList_AbsentExtensionPermits(t *testing.T) {
	var l TNAuthList
	ok, constrained := l.AuthorizesTN("+15551234567")
	if !ok || constrained {
		t.Fatalf("got ok=%v constrained=%v", ok, constrained)
	}
}
