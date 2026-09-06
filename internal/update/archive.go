package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"path"
	"strings"
)

// ValidateArchive rejects links, traversal and unexpected state/configuration
// payloads before a verified release is allowed to touch the filesystem.
func ValidateArchive(data []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	t := tar.NewReader(gz)
	seen := map[string]bool{}
	var size int64
	count := 0
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, "./")
		name = strings.TrimSuffix(name, "/")
		if name == "" || name == "." {
			continue
		}
		if path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\n\r\\") || name == ".." || strings.HasPrefix(name, "../") {
			return errors.New("unsafe release archive path")
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || seen[name] {
			return errors.New("release archive contains links or duplicate files")
		}
		allowed := name == "usr/bin/fastlane" || name == "etc/init.d/fastlane" || name == "etc/init.d/xray" || strings.HasPrefix(name, "usr/libexec/fastlane-") || strings.HasPrefix(name, "www/luci-static/resources/fastlane/") || strings.HasPrefix(name, "www/luci-static/resources/view/fastlane/") || name == "usr/share/luci/menu.d/luci-app-fastlane.json" || name == "usr/share/rpcd/acl.d/luci-app-fastlane.json"
		if !allowed {
			return errors.New("release archive contains files outside Fast Lane")
		}
		seen[name] = true
		size += h.Size
		count++
		if size > 256*1024*1024 || count > 2000 {
			return errors.New("release archive exceeds installation limits")
		}
	}
	if !seen["usr/bin/fastlane"] {
		return errors.New("release archive has no Fast Lane binary")
	}
	return nil
}
