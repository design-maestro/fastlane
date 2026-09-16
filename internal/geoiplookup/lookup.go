// Package geoiplookup resolves IP addresses against Xray's local geoip.dat
// without sending server addresses to an external geolocation service.
package geoiplookup

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// LookupFile returns ISO country codes for the requested addresses. Addresses
// not present in the database are omitted.
func LookupFile(path string, requested []netip.Addr) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wanted := make(map[netip.Addr]string, len(requested))
	for _, address := range requested {
		address = address.Unmap()
		if address.IsValid() {
			wanted[address] = address.String()
		}
	}
	result := make(map[string]string, len(wanted))
	for len(data) > 0 && len(result) < len(wanted) {
		field, wire, payload, rest, consumeErr := consumeField(data)
		if consumeErr != nil {
			return nil, fmt.Errorf("decode geoip.dat: %w", consumeErr)
		}
		data = rest
		if field != 1 || wire != 2 {
			continue
		}
		code, prefixes, reverse, parseErr := parseCountry(payload)
		if parseErr != nil {
			return nil, fmt.Errorf("decode geoip.dat country: %w", parseErr)
		}
		code = strings.ToUpper(strings.TrimSpace(code))
		if reverse || len(code) != 2 {
			continue
		}
		for address, text := range wanted {
			if _, found := result[text]; found {
				continue
			}
			for _, prefix := range prefixes {
				if prefix.Contains(address) {
					result[text] = code
					break
				}
			}
		}
	}
	return result, nil
}

func parseCountry(data []byte) (string, []netip.Prefix, bool, error) {
	var code string
	var prefixes []netip.Prefix
	var reverse bool
	for len(data) > 0 {
		field, wire, payload, rest, err := consumeField(data)
		if err != nil {
			return "", nil, false, err
		}
		data = rest
		switch {
		case field == 1 && wire == 2:
			code = string(payload)
		case field == 2 && wire == 2:
			prefix, ok, parseErr := parseCIDR(payload)
			if parseErr != nil {
				return "", nil, false, parseErr
			}
			if ok {
				prefixes = append(prefixes, prefix)
			}
		case field == 3 && wire == 0:
			value, size := binary.Uvarint(payload)
			if size <= 0 {
				return "", nil, false, fmt.Errorf("invalid reverse_match value")
			}
			reverse = value != 0
		}
	}
	return code, prefixes, reverse, nil
}

func parseCIDR(data []byte) (netip.Prefix, bool, error) {
	var rawIP []byte
	var bits uint64
	for len(data) > 0 {
		field, wire, payload, rest, err := consumeField(data)
		if err != nil {
			return netip.Prefix{}, false, err
		}
		data = rest
		switch {
		case field == 1 && wire == 2:
			rawIP = append([]byte(nil), payload...)
		case field == 2 && wire == 0:
			value, size := binary.Uvarint(payload)
			if size <= 0 {
				return netip.Prefix{}, false, fmt.Errorf("invalid CIDR prefix")
			}
			bits = value
		}
	}
	address, ok := netip.AddrFromSlice(rawIP)
	if !ok {
		return netip.Prefix{}, false, nil
	}
	address = address.Unmap()
	if bits > uint64(address.BitLen()) {
		return netip.Prefix{}, false, nil
	}
	return netip.PrefixFrom(address, int(bits)).Masked(), true, nil
}

func consumeField(data []byte) (field int, wire int, payload []byte, rest []byte, err error) {
	key, size := binary.Uvarint(data)
	if size <= 0 {
		return 0, 0, nil, nil, fmt.Errorf("invalid protobuf key")
	}
	field, wire = int(key>>3), int(key&7)
	data = data[size:]
	switch wire {
	case 0:
		_, valueSize := binary.Uvarint(data)
		if valueSize <= 0 {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf varint")
		}
		return field, wire, data[:valueSize], data[valueSize:], nil
	case 1:
		if len(data) < 8 {
			return 0, 0, nil, nil, fmt.Errorf("truncated protobuf fixed64")
		}
		return field, wire, data[:8], data[8:], nil
	case 2:
		length, lengthSize := binary.Uvarint(data)
		if lengthSize <= 0 || length > uint64(len(data)-lengthSize) {
			return 0, 0, nil, nil, fmt.Errorf("invalid protobuf length")
		}
		start := lengthSize
		end := start + int(length)
		return field, wire, data[start:end], data[end:], nil
	case 5:
		if len(data) < 4 {
			return 0, 0, nil, nil, fmt.Errorf("truncated protobuf fixed32")
		}
		return field, wire, data[:4], data[4:], nil
	default:
		return 0, 0, nil, nil, fmt.Errorf("unsupported protobuf wire type %d", wire)
	}
}
