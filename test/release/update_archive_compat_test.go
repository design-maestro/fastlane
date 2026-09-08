package release_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

func validateLegacyUpdateArchive(data []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(header.Name, "./"), "/")
		allowed := name == "usr/bin/fastlane" ||
			name == "etc/init.d/fastlane" ||
			name == "etc/init.d/xray" ||
			strings.HasPrefix(name, "usr/libexec/fastlane-") ||
			strings.HasPrefix(name, "www/luci-static/resources/fastlane/") ||
			strings.HasPrefix(name, "www/luci-static/resources/view/fastlane/") ||
			name == "usr/share/luci/menu.d/luci-app-fastlane.json" ||
			name == "usr/share/rpcd/acl.d/luci-app-fastlane.json"
		if !allowed {
			return fmt.Errorf("legacy updater rejects %s", name)
		}
	}
}
