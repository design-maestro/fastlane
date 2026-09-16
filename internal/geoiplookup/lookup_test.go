package geoiplookup

import (
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupFileMatchesIPv4AndIPv6(t *testing.T) {
	data := fieldBytes(1, countryMessage("CH", []testPrefix{
		{ip: netip.MustParseAddr("85.234.0.0"), bits: 16},
		{ip: netip.MustParseAddr("2001:db8::"), bits: 32},
	}))
	path := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := LookupFile(path, []netip.Addr{
		netip.MustParseAddr("85.234.103.60"),
		netip.MustParseAddr("2001:db8::42"),
		netip.MustParseAddr("192.0.2.1"),
	})
	if err != nil {
		t.Fatalf("LookupFile: %v", err)
	}
	if result["85.234.103.60"] != "CH" || result["2001:db8::42"] != "CH" {
		t.Fatalf("result = %#v", result)
	}
	if _, exists := result["192.0.2.1"]; exists {
		t.Fatalf("unmatched address unexpectedly resolved: %#v", result)
	}
}

type testPrefix struct {
	ip   netip.Addr
	bits uint64
}

func countryMessage(code string, prefixes []testPrefix) []byte {
	message := fieldBytes(1, []byte(code))
	for _, prefix := range prefixes {
		address := prefix.ip.AsSlice()
		cidr := append(fieldBytes(1, address), fieldVarint(2, prefix.bits)...)
		message = append(message, fieldBytes(2, cidr)...)
	}
	return message
}

func fieldBytes(number uint64, value []byte) []byte {
	buffer := appendVarint(nil, number<<3|2)
	buffer = appendVarint(buffer, uint64(len(value)))
	return append(buffer, value...)
}

func fieldVarint(number, value uint64) []byte {
	buffer := appendVarint(nil, number<<3)
	return appendVarint(buffer, value)
}

func appendVarint(buffer []byte, value uint64) []byte {
	var encoded [binary.MaxVarintLen64]byte
	size := binary.PutUvarint(encoded[:], value)
	return append(buffer, encoded[:size]...)
}
