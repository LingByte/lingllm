// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package historyinfo

import (
	"reflect"
	"strings"
	"testing"
)

func TestEntryFormat(t *testing.T) {
	cases := []struct {
		name string
		e    Entry
		want string
	}{
		{
			name: "uri only",
			e:    Entry{URI: "sip:a@x"},
			want: "<sip:a@x>",
		},
		{
			name: "uri + index",
			e:    Entry{URI: "sip:a@x", Index: "1"},
			want: "<sip:a@x>;index=1",
		},
		{
			name: "with Reason percent-encoded",
			e:    Entry{URI: "sip:a@x", Index: "2", ReasonHeader: "SIP;cause=302"},
			want: "<sip:a@x?Reason=SIP%3Bcause%3D302>;index=2",
		},
		{
			name: "trim angle brackets on input",
			e:    Entry{URI: "<sip:a@x>", Index: "1"},
			want: "<sip:a@x>;index=1",
		},
		{
			name: "empty drops",
			e:    Entry{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.e.Format()
			if got != tc.want {
				t.Fatalf("Format() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseChain_RoundTrip(t *testing.T) {
	// Two entries comma-separated; second carries percent-encoded
	// Reason. Verify parse → format yields an equivalent chain (the
	// chain SEQUENCE is what matters; ordering of internal params can
	// differ if we ever add more URI-headers).
	raw := `<sip:trunk@us.example>;index=1, <sip:agent@pool.us.example?Reason=SIP%3Bcause%3D480%3Btext%3D%22AI+Transfer%22>;index=2`
	got := ParseChain(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d (%+v)", len(got), got)
	}
	if got[0].URI != "sip:trunk@us.example" || got[0].Index != "1" {
		t.Errorf("entry[0] = %+v", got[0])
	}
	if got[1].URI != "sip:agent@pool.us.example" || got[1].Index != "2" {
		t.Errorf("entry[1] = %+v", got[1])
	}
	wantReason := `SIP;cause=480;text="AI Transfer"`
	if got[1].ReasonHeader != wantReason {
		t.Errorf("entry[1].ReasonHeader = %q, want %q", got[1].ReasonHeader, wantReason)
	}
}

func TestParseChain_Lenient(t *testing.T) {
	cases := []struct {
		in       string
		wantURIs []string
	}{
		{"", nil},
		{"   ", nil},
		{"<sip:a@x>", []string{"sip:a@x"}},
		{"<sip:a@x>;index=1\r\n<sip:b@y>;index=2", []string{"sip:a@x", "sip:b@y"}},
		// Garbage entries drop, valid stay.
		{"not-a-uri, <sip:a@x>", []string{"sip:a@x"}},
		// Comma inside URI-headers doesn't split. (Carriers do this
		// when Reason contains a comma in the text= portion.)
		{`<sip:a@x?Reason=SIP%3Bcause%3D302>;index=1, <sip:b@y>;index=2`, []string{"sip:a@x", "sip:b@y"}},
	}
	for _, tc := range cases {
		got := ParseChain(tc.in)
		var uris []string
		for _, e := range got {
			uris = append(uris, e.URI)
		}
		if !reflect.DeepEqual(uris, tc.wantURIs) {
			t.Errorf("ParseChain(%q) URIs = %v, want %v", tc.in, uris, tc.wantURIs)
		}
	}
}

func TestNextIndex(t *testing.T) {
	cases := []struct {
		chain []Entry
		want  string
	}{
		{nil, "1"},
		{[]Entry{{Index: "1"}}, "2"},
		{[]Entry{{Index: "1"}, {Index: "2"}}, "3"},
		{[]Entry{{Index: "1"}, {Index: "1.1"}}, "2"}, // dotted children don't count at top level
		{[]Entry{{Index: "5"}}, "6"},
		{[]Entry{{Index: "garbage"}}, "1"},
	}
	for _, tc := range cases {
		if got := NextIndex(tc.chain); got != tc.want {
			t.Errorf("NextIndex(%+v) = %q, want %q", tc.chain, got, tc.want)
		}
	}
}

func TestAppendTransferEntry(t *testing.T) {
	t.Run("empty inbound chain synthesises root", func(t *testing.T) {
		out := AppendTransferEntry(nil, "sip:trunk@us", "sip:agent@pool", "SIP;cause=302")
		if len(out) != 2 {
			t.Fatalf("got %d entries, want 2", len(out))
		}
		if out[0].URI != "sip:trunk@us" || out[0].Index != "1" {
			t.Errorf("root entry = %+v", out[0])
		}
		if out[1].URI != "sip:agent@pool" || out[1].Index != "2" || out[1].ReasonHeader != "SIP;cause=302" {
			t.Errorf("new entry = %+v", out[1])
		}
	})

	t.Run("extends existing chain", func(t *testing.T) {
		inbound := []Entry{
			{URI: "sip:sbc-upstream@carrier", Index: "1"},
			{URI: "sip:trunk@us", Index: "2"},
		}
		out := AppendTransferEntry(inbound, "sip:trunk@us", "sip:agent@pool", "")
		if len(out) != 3 {
			t.Fatalf("got %d entries, want 3 (do NOT re-add root when chain non-empty)", len(out))
		}
		if out[2].Index != "3" {
			t.Errorf("new entry index = %q, want %q", out[2].Index, "3")
		}
	})

	t.Run("empty new target is no-op", func(t *testing.T) {
		inbound := []Entry{{URI: "sip:trunk@us", Index: "1"}}
		out := AppendTransferEntry(inbound, "sip:trunk@us", "", "")
		if !reflect.DeepEqual(out, inbound) {
			t.Errorf("expected no-op, got %+v", out)
		}
	})
}

func TestDiversion_FormatParse(t *testing.T) {
	d := Diversion{
		URI:     "sip:trunk@us",
		Reason:  DiversionDeflection,
		Counter: 1,
		Privacy: "off",
	}
	got := d.Format()
	want := "<sip:trunk@us>;reason=deflection;counter=1;privacy=off"
	if got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}

	parsed := ParseDiversionChain(got)
	if len(parsed) != 1 {
		t.Fatalf("Parse returned %d entries", len(parsed))
	}
	if !reflect.DeepEqual(parsed[0], d) {
		t.Errorf("round-trip mismatch:\n  got  = %+v\n  want = %+v", parsed[0], d)
	}
}

func TestAppendDiversionEntry(t *testing.T) {
	out := AppendDiversionEntry(nil, "sip:trunk@us", "")
	if len(out) != 1 || out[0].Reason != DiversionUnconditional || out[0].Counter != 1 {
		t.Fatalf("first append = %+v", out)
	}

	// Second extension increments counter.
	out2 := AppendDiversionEntry(out, "sip:trunk2@us", DiversionDeflection)
	if len(out2) != 2 || out2[1].Counter != 2 || out2[1].Reason != "deflection" {
		t.Fatalf("second append = %+v", out2)
	}
}

func TestExtractURIFromToHeader(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"<sip:alice@biz>", "sip:alice@biz"},
		{`"Alice" <sip:alice@biz>;tag=abc`, "sip:alice@biz"},
		{"sip:alice@biz;tag=abc", "sip:alice@biz"},
		{"garbage", "garbage"}, // best-effort; caller should validate.
	}
	for _, tc := range cases {
		if got := ExtractURIFromToHeader(tc.in); got != tc.want {
			t.Errorf("ExtractURIFromToHeader(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatChain_RoundTripChain(t *testing.T) {
	// End-to-end: build chain via AppendTransferEntry then re-parse.
	chain := AppendTransferEntry(nil, "sip:trunk@us", "sip:agent@pool", "SIP;cause=480")
	hdr := FormatChain(chain)
	if !strings.Contains(hdr, "index=1") || !strings.Contains(hdr, "index=2") {
		t.Fatalf("FormatChain output missing indices: %q", hdr)
	}
	if !strings.Contains(hdr, "Reason=SIP%3Bcause%3D480") {
		t.Fatalf("FormatChain output missing percent-encoded Reason: %q", hdr)
	}
	parsed := ParseChain(hdr)
	if len(parsed) != 2 {
		t.Fatalf("re-parse returned %d entries (%q)", len(parsed), hdr)
	}
	if parsed[1].ReasonHeader != "SIP;cause=480" {
		t.Errorf("re-parsed reason = %q", parsed[1].ReasonHeader)
	}
}
