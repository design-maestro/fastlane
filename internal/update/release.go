// Package update manages explicit, release-pinned Fast Lane updates.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const Repository = "design-maestro/fastlane"
const apiBase = "https://api.github.com/repos/" + Repository

type Asset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

type Release struct {
	ID         int64   `json:"id"`
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Candidate struct {
	ID        int64  `json:"id"`
	Tag       string `json:"tag"`
	Version   string `json:"version"`
	Page      string `json:"page"`
	Installer Asset  `json:"installer"`
	Package   Asset  `json:"package"`
}

type ChannelError struct{ Code, Message string }

func (e *ChannelError) Error() string { return e.Message }

func NewClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !trustedDownload(req.URL) {
			return errors.New("unsafe release redirect")
		}
		return nil
	}}
}

func trustedDownload(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var localVersion = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)(?:[-+].*)?$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func Compare(candidate, current string) (int, error) {
	if !stableVersion.MatchString(candidate) {
		return 0, errors.New("release must have a stable vMAJOR.MINOR.PATCH tag")
	}
	base := localVersion.FindStringSubmatch(current)
	if base == nil {
		return 1, nil
	} // A local development build can move to a stable release.
	a, b := strings.Split(strings.TrimPrefix(candidate, "v"), "."), strings.Split(base[1], ".")
	for i := range a {
		x, err := strconv.ParseUint(a[i], 10, 64)
		if err != nil {
			return 0, err
		}
		y, err := strconv.ParseUint(b[i], 10, 64)
		if err != nil {
			return 0, err
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	if !stableVersion.MatchString(current) {
		return 1, nil
	}
	return 0, nil
}

func Architecture() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64_cortex-a53"
	case "amd64":
		return "x86_64"
	case "mipsle":
		return "mipsel_24kc"
	}
	return ""
}

func Fetch(ctx context.Context, client *http.Client, tag string) (Release, error) {
	endpoint := apiBase + "/releases/latest"
	if tag != "" {
		if !stableVersion.MatchString(tag) {
			return Release{}, errors.New("invalid release tag")
		}
		endpoint = apiBase + "/releases/tags/" + tag
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Fast-Lane-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, &ChannelError{"network_error", "Не удалось связаться с GitHub. Проверьте интернет и повторите проверку."}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 404:
		return Release{}, &ChannelError{"unavailable", "Релиз пока не опубликован или репозиторий приватный. Для обновления нужен доступный релиз Fast Lane."}
	case 403, 429:
		return Release{}, &ChannelError{"rate_limited", "GitHub временно ограничил запросы. Повторите проверку позже."}
	case 200:
	default:
		return Release{}, &ChannelError{"network_error", "GitHub не смог вернуть релиз. Повторите проверку позже."}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		return Release{}, err
	}
	if len(data) > 2*1024*1024 {
		return Release{}, errors.New("release metadata exceeds size limit")
	}
	var release Release
	if err = json.Unmarshal(data, &release); err != nil {
		return release, errors.New("invalid GitHub release metadata")
	}
	return release, nil
}

func Select(release Release, arch string) (Candidate, error) {
	if release.ID <= 0 || release.Draft || release.Prerelease || !stableVersion.MatchString(release.Tag) || !strings.HasPrefix(release.Tag, "v") {
		return Candidate{}, errors.New("expected a published stable vMAJOR.MINOR.PATCH release")
	}
	if arch != "aarch64_cortex-a53" && arch != "x86_64" && arch != "mipsel_24kc" {
		return Candidate{}, &ChannelError{"unsupported", "Для этой архитектуры нет поддерживаемой сборки Fast Lane."}
	}
	version := strings.TrimPrefix(release.Tag, "v")
	result := Candidate{ID: release.ID, Tag: release.Tag, Version: version, Page: "https://github.com/" + Repository + "/releases/tag/" + release.Tag}
	packageName := "fastlane_" + version + "_" + arch + ".tar.gz"
	found := map[string]Asset{}
	for _, asset := range release.Assets {
		if asset.Name != "install.sh" && asset.Name != packageName && asset.Name != packageName+".sha256" {
			continue
		}
		if _, duplicate := found[asset.Name]; duplicate {
			return Candidate{}, errors.New("duplicate release asset")
		}
		expected := "https://github.com/" + Repository + "/releases/download/" + release.Tag + "/" + asset.Name
		if asset.ID <= 0 || asset.URL != expected || !digestPattern.MatchString(asset.Digest) {
			return Candidate{}, errors.New("release asset has no valid SHA-256 or trusted URL")
		}
		found[asset.Name] = asset
	}
	if len(found) != 3 {
		return Candidate{}, &ChannelError{"incomplete", "Релиз ещё не готов: отсутствует установщик, сборка для роутера или контрольные суммы."}
	}
	result.Installer, result.Package = found["install.sh"], found[packageName]
	return result, nil
}

func Download(ctx context.Context, client *http.Client, asset Asset, limit int64) ([]byte, error) {
	u, err := url.Parse(asset.URL)
	if err != nil || !trustedDownload(u) || u.Hostname() != "github.com" || !strings.HasPrefix(u.Path, "/"+Repository+"/releases/download/") || !digestPattern.MatchString(asset.Digest) {
		return nil, errors.New("untrusted asset")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("release download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("release download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release asset exceeds size limit")
	}
	sum := sha256.Sum256(data)
	if "sha256:"+hex.EncodeToString(sum[:]) != asset.Digest {
		return nil, errors.New("release SHA-256 mismatch; installation blocked")
	}
	return data, nil
}
