package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func digest(b string) string {
	sum := sha256.Sum256([]byte(b))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func fixture() Release {
	r := Release{ID: 10, Tag: "v1.2.3"}
	for i, name := range []string{"install.sh", "fastlane_1.2.3_x86_64.tar.gz", "fastlane_1.2.3_x86_64.tar.gz.sha256"} {
		r.Assets = append(r.Assets, Asset{ID: int64(i + 1), Name: name, URL: "https://github.com/" + Repository + "/releases/download/v1.2.3/" + name, Digest: digest("fixture")})
	}
	return r
}
func clientFor(r Release, code int) *http.Client {
	return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		body := []byte("fixture")
		if strings.Contains(req.URL.Host, "api.github.com") {
			body, _ = json.Marshal(r)
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
}
func manager(t *testing.T) *Manager {
	t.Helper()
	m := &Manager{Dir: t.TempDir(), Current: "1.2.2", Arch: "x86_64", Client: clientFor(fixture(), 200)}
	if err := os.Chmod(m.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	m.Spawn = func(string, string) (int, error) { return os.Getpid(), nil }
	return m
}
func run(t *testing.T, m *Manager, op string, id int64) State {
	t.Helper()
	s, err := m.Start(op, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Run(context.Background(), op, s.Token); err != nil {
		t.Fatal(err)
	}
	s, err = m.Status()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		next, current string
		want          int
	}{{"1.2.3", "1.2.2", 1}, {"1.2.3", "v1.2.3", 0}, {"1.2.3", "1.3.0", -1}, {"1.2.3", "dev", 1}, {"1.2.3", "1.2.3-local", 1}, {"1.2.3", "1.3.0-local", -1}, {"1.2.10", "1.2.9", 1}} {
		got, err := Compare(tc.next, tc.current)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got %d %v", tc, got, err)
		}
	}
	for _, v := range []string{"main", "v1.2.3-beta.1", "1.02.3", "1.2.999999999999999999999999999"} {
		if _, err := Compare(v, "1.2.2"); err == nil {
			t.Fatal("accepted invalid version", v)
		}
	}
}

func TestSelectRejectsUnsafeAndIncompleteReleases(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Release)
	}{
		{"draft", func(r *Release) { r.Draft = true }}, {"prerelease", func(r *Release) { r.Prerelease = true }},
		{"tag", func(r *Release) { r.Tag = "v1.2.3;reboot" }}, {"digest", func(r *Release) { r.Assets[0].Digest = "" }},
		{"foreign URL", func(r *Release) { r.Assets[0].URL = "https://evil.example/install.sh" }},
		{"latest race", func(r *Release) {
			r.Assets[0].URL = "https://github.com/" + Repository + "/releases/latest/download/install.sh"
		}},
		{"missing", func(r *Release) { r.Assets = r.Assets[:2] }}, {"duplicate", func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture()
			tc.edit(&r)
			if _, err := Select(r, "x86_64"); err == nil {
				t.Fatal("accepted unsafe release")
			}
		})
	}
	if _, err := Select(fixture(), "armv7"); err == nil {
		t.Fatal("accepted unsupported architecture")
	}
	if _, err := Select(fixture(), "x86_64"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadChecksDigestSizeAndRedirects(t *testing.T) {
	c, _ := Select(fixture(), "x86_64")
	if _, err := Download(context.Background(), clientFor(fixture(), 200), c.Installer, 100); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		code   int
		limit  int64
		digest string
	}{{500, 100, c.Installer.Digest}, {200, 2, c.Installer.Digest}, {200, 100, digest("different")}} {
		a := c.Installer
		a.Digest = tc.digest
		if _, err := Download(context.Background(), clientFor(fixture(), tc.code), a, tc.limit); err == nil {
			t.Fatal("accepted corrupt download")
		}
	}
	for _, target := range []string{"http://github.com/x", "https://evil.example/x", "https://github.com:444/x", "https://user@github.com/x"} {
		u, _ := url.Parse(target)
		if err := NewClient().CheckRedirect(&http.Request{URL: u}, nil); err == nil {
			t.Fatal("accepted redirect", target)
		}
	}
}

func TestCheckStatesAndStatusNeverFetches(t *testing.T) {
	for _, tc := range []struct {
		version string
		code    int
		want    string
	}{{"1.2.2", 200, "available"}, {"1.2.3", 200, "current"}, {"1.3.0", 200, "newer"}, {"1.2.2", 404, "unavailable"}, {"1.2.2", 403, "rate_limited"}, {"1.2.2", 500, "network_error"}} {
		m := manager(t)
		m.Current = tc.version
		m.Client = clientFor(fixture(), tc.code)
		s := run(t, m, "check", 0)
		if s.Status != tc.want {
			t.Fatalf("%+v: %+v", tc, s)
		}
		m.Client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("status used network"); return nil, nil })}
		if _, err := m.Status(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallRequiresFreshExplicitCandidateAndPreventsDuplicates(t *testing.T) {
	m := manager(t)
	if _, err := m.Start("install", 10); err == nil {
		t.Fatal("installed without check")
	}
	s := run(t, m, "check", 0)
	if _, err := m.Start("install", 11); err == nil {
		t.Fatal("installed unapproved release")
	}
	s.CheckedAt = time.Now().Add(-time.Hour)
	if err := m.write(s); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("install", 10); err == nil {
		t.Fatal("installed stale candidate")
	}
	run(t, m, "check", 0)
	calls := 0
	m.Spawn = func(string, string) (int, error) { calls++; return os.Getpid(), nil }
	first, err := m.Start("install", 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Start("install", 10)
	if err != nil || first.Token != second.Token || calls != 1 {
		t.Fatalf("duplicate: %d %v", calls, err)
	}
	m.Install = func(context.Context, Candidate, []byte) error { return nil }
	if err = m.Run(context.Background(), "install", first.Token); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Status()
	if s.Status != "updated" {
		t.Fatal(s)
	}
	if err = m.Run(context.Background(), "install", first.Token); err == nil {
		t.Fatal("replayed completed worker")
	}
}

func TestInstallRefusesChangedReleaseAndReportsFailure(t *testing.T) {
	for _, changed := range []bool{false, true} {
		m := manager(t)
		run(t, m, "check", 0)
		calls := 0
		m.Install = func(context.Context, Candidate, []byte) error { calls++; return errors.New("fixture failure") }
		if changed {
			r := fixture()
			r.Assets[0].ID = 99
			m.Client = clientFor(r, 200)
		}
		s := run(t, m, "install", 10)
		if s.Status != "error" || (changed && calls != 0) || (!changed && calls != 1) {
			t.Fatalf("state %+v calls %d", s, calls)
		}
	}
}

func TestRecoverDeadWorkerAndSpawnFailure(t *testing.T) {
	m := manager(t)
	s, err := m.Start("check", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.PID = 0
	s.StartedAt = time.Now().Add(-time.Minute)
	if err = m.write(s); err != nil {
		t.Fatal(err)
	}
	s, err = m.Status()
	if err != nil || s.Status != "interrupted" {
		t.Fatal(s, err)
	}
	m.Spawn = func(string, string) (int, error) { return 0, errors.New("fixture") }
	if _, err = m.Start("check", 0); err == nil {
		t.Fatal("spawn failure ignored")
	}
	s, _ = m.Status()
	if s.Status != "error" {
		t.Fatal(s)
	}
}
