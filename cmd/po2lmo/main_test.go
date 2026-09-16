package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompileProducesLuCIArchive(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "fastlane.po")
	output := filepath.Join(dir, "fastlane.ru.lmo")
	po := "msgid \"\"\nmsgstr \"\"\n\"Language: ru\\n\"\n\nmsgid \"Settings\"\nmsgstr \"Настройки\"\n"
	if err := os.WriteFile(input, []byte(po), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := compile(input, output); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) < 20 {
		t.Fatalf("archive too short: %d", len(archive))
	}
	indexOffset := int(binary.BigEndian.Uint32(archive[len(archive)-4:]))
	if indexOffset <= 0 || indexOffset+20 != len(archive) {
		t.Fatalf("unexpected index offset %d for %d byte archive", indexOffset, len(archive))
	}
	entry := archive[indexOffset : indexOffset+16]
	if got, want := binary.BigEndian.Uint32(entry[0:4]), sfhHash([]byte("Settings")); got != want {
		t.Fatalf("key hash = %08x, want %08x", got, want)
	}
	valueOffset := int(binary.BigEndian.Uint32(entry[8:12]))
	valueLength := int(binary.BigEndian.Uint32(entry[12:16]))
	if got := string(archive[valueOffset : valueOffset+valueLength]); got != "Настройки" {
		t.Fatalf("translation = %q", got)
	}
}

func TestParseContextAndPlural(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "plural.po")
	po := "msgctxt \"menu\"\nmsgid \"server\"\nmsgid_plural \"servers\"\nmsgstr[0] \"сервер\"\nmsgstr[1] \"сервера\"\n"
	if err := os.WriteFile(input, []byte(po), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	messages, err := parsePO(file)
	if err != nil {
		t.Fatal(err)
	}
	data, entries := encodeMessages(messages)
	if len(entries) != 2 || len(data) == 0 {
		t.Fatalf("got %d entries and %d data bytes", len(entries), len(data))
	}
}

func TestEncodeMessagesIncludesPluralFormulaHeader(t *testing.T) {
	messages := []message{{id: "", values: map[int]string{0: "Language: ru\nPlural-Forms: nplurals=3; plural=(n%10==1 ? 0 : 1);\n"}, seen: true}}
	data, entries := encodeMessages(messages)
	if len(entries) != 1 || entries[0].keyID != 0 || entries[0].valueN != 0 {
		t.Fatalf("plural formula entry = %+v", entries)
	}
	if got := string(data[:entries[0].length]); got != "nplurals=3; plural=(n%10==1 ? 0 : 1);" {
		t.Fatalf("plural formula = %q", got)
	}
}

func TestRepositoryCatalogContainsAWGTranslation(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	inputPath := filepath.Join(filepath.Dir(source), "..", "..", "luci-app-fastlane", "po", "ru", "fastlane.po")
	input, err := os.Open(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	messages, err := parsePO(input)
	if err != nil {
		t.Fatal(err)
	}

	var translated bool
	hashes := make(map[uint32]string)
	for _, msg := range messages {
		if msg.id == "AWG 2.0" && msg.values[0] == "AWG 2.0" {
			translated = true
		}
		key := msg.id
		if msg.context != "" {
			key = msg.context + "\x01" + key
		}
		hash := sfhHash([]byte(key))
		if previous, exists := hashes[hash]; exists && previous != key {
			t.Fatalf("catalog hash collision %08x between %q and %q", hash, previous, key)
		}
		hashes[hash] = key
	}
	if !translated {
		t.Fatal("repository catalog is missing AWG 2.0 translation")
	}
}
