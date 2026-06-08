// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

package identity

import (
	"net"
	"reflect"
	"testing"
)

func TestParsePAI_Variants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Asserted
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "quoted display-name + sip uri",
			in:   `"Alice" <sip:alice@biz.example>`,
			want: []Asserted{{URI: "sip:alice@biz.example", DisplayName: "Alice", Scheme: "sip"}},
		},
		{
			name: "bare sip uri",
			in:   `<sip:+8613800138000@trust.example>`,
			want: []Asserted{{URI: "sip:+8613800138000@trust.example", DisplayName: "", Scheme: "sip"}},
		},
		{
			name: "two rows comma-separated (sip + tel)",
			in:   `"Alice" <sip:alice@biz.example>, <tel:+8613800138000>`,
			want: []Asserted{
				{URI: "sip:alice@biz.example", DisplayName: "Alice", Scheme: "sip"},
				{URI: "tel:+8613800138000", DisplayName: "", Scheme: "tel"},
			},
		},
		{
			name: "comma inside display-name does NOT split",
			in:   `"Smith, Jr" <sip:smith@biz.example>`,
			want: []Asserted{{URI: "sip:smith@biz.example", DisplayName: "Smith, Jr", Scheme: "sip"}},
		},
		{
			name: "escaped quote in display-name",
			in:   `"He said \"hi\"" <sip:hi@biz.example>`,
			want: []Asserted{{URI: "sip:hi@biz.example", DisplayName: `He said "hi"`, Scheme: "sip"}},
		},
		{
			name: "scheme not allowed (http) is dropped",
			in:   `<http://evil.example/>`,
			want: nil,
		},
		{
			name: "addr-spec without angle brackets",
			in:   `sip:bob@biz.example`,
			want: []Asserted{{URI: "sip:bob@biz.example", DisplayName: "", Scheme: "sip"}},
		},
		{
			name: "folded multi-line",
			in:   "<sip:a@x>\r\n<tel:+123>",
			want: []Asserted{
				{URI: "sip:a@x", DisplayName: "", Scheme: "sip"},
				{URI: "tel:+123", DisplayName: "", Scheme: "tel"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePAI(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParsePAI(%q):\n  got  = %+v\n  want = %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestAssertedFormatHeader(t *testing.T) {
	cases := []struct {
		a    Asserted
		want string
	}{
		{Asserted{}, ""},
		{Asserted{URI: "sip:a@x"}, "<sip:a@x>"},
		{Asserted{URI: "<sip:a@x>"}, "<sip:a@x>"},
		{Asserted{URI: "sip:a@x", DisplayName: "Alice"}, `"Alice" <sip:a@x>`},
		{Asserted{URI: `sip:a@x`, DisplayName: `He "said" "ok"`}, `"He \"said\" \"ok\"" <sip:a@x>`},
	}
	for _, tc := range cases {
		if got := tc.a.FormatHeader(); got != tc.want {
			t.Errorf("Asserted{%+v}.FormatHeader() = %q, want %q", tc.a, got, tc.want)
		}
	}
}

func TestParsePrivacy(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"id", []string{"id"}},
		{"id;header", []string{"id", "header"}},
		{" ID ; Header ", []string{"id", "header"}},
		{";;id;;", []string{"id"}},
	}
	for _, tc := range cases {
		got := ParsePrivacy(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParsePrivacy(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPrivacyRequestsID(t *testing.T) {
	cases := []struct {
		in   []string
		want bool
	}{
		{nil, false},
		{[]string{"id"}, true},
		{[]string{"header"}, true},
		{[]string{"user"}, true},
		{[]string{"id", "none"}, false},   // none overrides
		{[]string{"session"}, false},      // session alone doesn't hide PAI
		{[]string{"critical"}, false},     // critical alone is just a flag
	}
	for _, tc := range cases {
		if got := PrivacyRequestsID(tc.in); got != tc.want {
			t.Errorf("PrivacyRequestsID(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPeerIsTrusted(t *testing.T) {
	t.Run("empty allow-list trusts any", func(t *testing.T) {
		SetTrustDomainsForTest([]string{})
		t.Cleanup(func() { SetTrustDomainsForTest(nil) })
		if !PeerIsTrusted(&net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 5060}) {
			t.Fatal("empty list should trust")
		}
	})
	t.Run("host-only match", func(t *testing.T) {
		SetTrustDomainsForTest([]string{"10.0.0.5", "carrier.example"})
		t.Cleanup(func() { SetTrustDomainsForTest(nil) })
		ok := PeerIsTrusted(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5060})
		if !ok {
			t.Fatal("10.0.0.5 should be trusted regardless of port")
		}
		bad := PeerIsTrusted(&net.UDPAddr{IP: net.ParseIP("10.0.0.6"), Port: 5060})
		if bad {
			t.Fatal("10.0.0.6 should NOT be trusted")
		}
	})
	t.Run("host:port exact match", func(t *testing.T) {
		SetTrustDomainsForTest([]string{"10.0.0.5:5061"})
		t.Cleanup(func() { SetTrustDomainsForTest(nil) })
		if PeerIsTrusted(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5060}) {
			t.Fatal("port 5060 should NOT match :5061 entry")
		}
		if !PeerIsTrusted(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 5061}) {
			t.Fatal("port 5061 should match :5061 entry")
		}
	})
	t.Run("nil addr with non-empty list is rejected", func(t *testing.T) {
		SetTrustDomainsForTest([]string{"10.0.0.5"})
		t.Cleanup(func() { SetTrustDomainsForTest(nil) })
		if PeerIsTrusted(nil) {
			t.Fatal("nil addr should not be trusted when list is set")
		}
	})
}

func TestFormatPrivacyHeader(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"id"}, "id"},
		{[]string{"id", "header"}, "id;header"},
		{[]string{"  ID ", "", "Header"}, "id;header"},
	}
	for _, tc := range cases {
		if got := FormatPrivacyHeader(tc.in); got != tc.want {
			t.Errorf("FormatPrivacyHeader(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
