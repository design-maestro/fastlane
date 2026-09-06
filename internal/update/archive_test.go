package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func archiveFixture(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	for _, h := range headers {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestValidateArchive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []*tar.Header
		valid   bool
	}{
		{"valid", []*tar.Header{{Name: "./usr/bin/fastlane", Typeflag: tar.TypeReg, Mode: 0755}}, true},
		{"empty", nil, false},
		{"secret", []*tar.Header{{Name: "etc/fastlane/settings.json", Typeflag: tar.TypeReg}}, false},
		{"traversal", []*tar.Header{{Name: "../usr/bin/fastlane", Typeflag: tar.TypeReg}}, false},
		{"absolute", []*tar.Header{{Name: "/usr/bin/fastlane", Typeflag: tar.TypeReg}}, false},
		{"link", []*tar.Header{{Name: "usr/bin/fastlane", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, false},
		{"duplicate", []*tar.Header{{Name: "usr/bin/fastlane", Typeflag: tar.TypeReg}, {Name: "usr/bin/fastlane", Typeflag: tar.TypeReg}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArchive(archiveFixture(t, tc.headers...))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
