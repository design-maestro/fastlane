// Command po2lmo compiles gettext PO catalogs into the LMO format used by LuCI.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type message struct {
	context string
	id      string
	plural  string
	values  map[int]string
	seen    bool
}

type indexEntry struct {
	keyID  uint32
	valueN uint32
	offset uint32
	length uint32
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s input.po output.lmo\n", os.Args[0])
		os.Exit(2)
	}

	if err := compile(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "po2lmo: %v\n", err)
		os.Exit(1)
	}
}

func compile(inputPath, outputPath string) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()

	messages, err := parsePO(input)
	if err != nil {
		return err
	}

	data, entries := encodeMessages(messages)
	if len(entries) == 0 {
		return errors.New("catalog has no translated messages")
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].keyID < entries[j].keyID })
	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(outputPath)
		}
	}()
	defer output.Close()

	if _, err := output.Write(data); err != nil {
		return err
	}
	for _, entry := range entries {
		for _, value := range []uint32{entry.keyID, entry.valueN, entry.offset, entry.length} {
			if err := binary.Write(output, binary.BigEndian, value); err != nil {
				return err
			}
		}
	}
	if err := binary.Write(output, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func parsePO(input *os.File) ([]message, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var messages []message
	current := message{values: make(map[int]string)}
	field := ""
	valueIndex := 0
	flush := func() {
		if current.seen {
			messages = append(messages, current)
		}
		current = message{values: make(map[int]string)}
		field = ""
		valueIndex = 0
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "msgctxt "):
			field = "context"
			current.seen = true
			current.context, _ = quoted(strings.TrimSpace(strings.TrimPrefix(line, "msgctxt")))
		case strings.HasPrefix(line, "msgid_plural "):
			field = "plural"
			current.seen = true
			current.plural, _ = quoted(strings.TrimSpace(strings.TrimPrefix(line, "msgid_plural")))
		case strings.HasPrefix(line, "msgid "):
			if current.seen && (current.id != "" || len(current.values) > 0) {
				flush()
			}
			field = "id"
			current.seen = true
			current.id, _ = quoted(strings.TrimSpace(strings.TrimPrefix(line, "msgid")))
		case strings.HasPrefix(line, "msgstr["):
			end := strings.IndexByte(line, ']')
			if end < 7 {
				return nil, fmt.Errorf("invalid plural translation %q", line)
			}
			index, err := strconv.Atoi(line[7:end])
			if err != nil || index < 0 {
				return nil, fmt.Errorf("invalid plural index in %q", line)
			}
			valueIndex = index
			field = "value"
			current.seen = true
			current.values[index], _ = quoted(strings.TrimSpace(line[end+1:]))
		case strings.HasPrefix(line, "msgstr "):
			valueIndex = 0
			field = "value"
			current.seen = true
			current.values[0], _ = quoted(strings.TrimSpace(strings.TrimPrefix(line, "msgstr")))
		case strings.HasPrefix(line, "\""):
			part, err := quoted(line)
			if err != nil {
				return nil, err
			}
			switch field {
			case "context":
				current.context += part
			case "id":
				current.id += part
			case "plural":
				current.plural += part
			case "value":
				current.values[valueIndex] += part
			default:
				return nil, fmt.Errorf("orphaned continuation %q", line)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return messages, nil
}

func quoted(value string) (string, error) {
	decoded, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid PO string %q: %w", value, err)
	}
	return decoded, nil
}

func encodeMessages(messages []message) ([]byte, []indexEntry) {
	var data []byte
	var entries []indexEntry
	for _, msg := range messages {
		if msg.id == "" {
			continue
		}
		valueN := len(msg.values)
		for index := 0; index < valueN; index++ {
			value := msg.values[index]
			if value == "" {
				continue
			}
			key := msg.id
			if msg.context != "" {
				key = msg.context + "\x01" + key
			}
			if msg.plural != "" {
				key += "\x02" + strconv.Itoa(index)
			}
			if sfhHash([]byte(key)) == sfhHash([]byte(value)) {
				continue
			}
			entries = append(entries, indexEntry{
				keyID:  sfhHash([]byte(key)),
				valueN: uint32(valueN),
				offset: uint32(len(data)),
				length: uint32(len(value)),
			})
			data = append(data, value...)
			for len(data)%4 != 0 {
				data = append(data, 0)
			}
		}
	}
	return data, entries
}

// sfhHash matches LuCI's canonical SuperFastHash implementation. The LuCI
// compatibility reference is Copyright (C) 2009-2012 Jo-Philipp Wich and is
// licensed under Apache-2.0; see THIRD_PARTY_NOTICES.md.
func sfhHash(data []byte) uint32 {
	if len(data) == 0 {
		return 0
	}
	hash := uint32(len(data))
	remaining := len(data) & 3
	blocks := len(data) >> 2
	offset := 0
	get16 := func(at int) uint32 { return uint32(data[at]) | uint32(data[at+1])<<8 }
	for range blocks {
		hash += get16(offset)
		tmp := (get16(offset+2) << 11) ^ hash
		hash = (hash << 16) ^ tmp
		offset += 4
		hash += hash >> 11
	}
	switch remaining {
	case 3:
		hash += get16(offset)
		hash ^= hash << 16
		hash ^= uint32(int32(int8(data[offset+2]))) << 18
		hash += hash >> 11
	case 2:
		hash += get16(offset)
		hash ^= hash << 11
		hash += hash >> 17
	case 1:
		hash += uint32(int32(int8(data[offset])))
		hash ^= hash << 10
		hash += hash >> 1
	}
	hash ^= hash << 3
	hash += hash >> 5
	hash ^= hash << 4
	hash += hash >> 17
	hash ^= hash << 25
	hash += hash >> 6
	return hash
}
