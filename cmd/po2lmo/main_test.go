package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
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
