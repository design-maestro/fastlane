package app

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestURLTestPortLocksCoordinateAcrossProcessesAndCleanFiles(t *testing.T) {
	root := t.TempDir()
	ports := []int{43123, 43124}
	locks, acquired, err := acquireURLTestPortLocksAt(root, ports...)
	if err != nil {
		t.Fatalf("acquire parent locks: %v", err)
	}
	if !acquired {
		t.Fatal("expected parent process to acquire locks")
	}

	runPortLockHelper(t, root, ports, false)
	releaseURLTestPortLocks(root, locks)

	assertOnlyURLTestGuardRemains(t, root)
	stalePath := filepath.Join(root, "49999.lock")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	runPortLockHelper(t, root, ports, true)
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale lock to be removed, got %v", err)
	}
	assertOnlyURLTestGuardRemains(t, root)
}

func TestReserveURLTestPortPairSkipsLockedAndBusyCandidates(t *testing.T) {
	root := t.TempDir()
	lockedPorts := []int{urlTestPortMin, urlTestPortMin + 1}
	locks, acquired, err := acquireURLTestPortLocksAt(root, lockedPorts...)
	if err != nil {
		t.Fatalf("lock first candidate pair: %v", err)
	}
	if !acquired {
		t.Fatal("expected first candidate pair lock")
	}
	defer releaseURLTestPortLocks(root, locks)

	busyPort := urlTestPortMin + 2
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(busyPort)))
	if err != nil {
		t.Fatalf("occupy second candidate SOCKS port: %v", err)
	}
	defer listener.Close()

	socksPort, httpPort, release, err := reserveURLTestPortPairAt(root, 0)
	if err != nil {
		t.Fatalf("reserve URL test pair: %v", err)
	}
	defer release()
	if socksPort != urlTestPortMin+4 || httpPort != urlTestPortMin+5 {
		t.Fatalf("reserved pair = %d/%d; want first unlocked and available pair %d/%d", socksPort, httpPort, urlTestPortMin+4, urlTestPortMin+5)
	}

	for _, port := range []int{socksPort, httpPort} {
		probe, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatalf("reservation leaked availability listener on %d: %v", port, err)
		}
		_ = probe.Close()
	}
}

func TestReserveURLTestPortPairsStayUniqueForConcurrentBatch(t *testing.T) {
	root := t.TempDir()
	type reservation struct {
		socks   int
		http    int
		release func()
		err     error
	}
	results := make(chan reservation, autoProbeConcurrency)
	for index := 0; index < autoProbeConcurrency; index++ {
		index := index
		go func() {
			socksPort, httpPort, release, err := reserveURLTestPortPairAt(root, index)
			results <- reservation{socks: socksPort, http: httpPort, release: release, err: err}
		}()
	}

	seen := make(map[int]struct{}, autoProbeConcurrency*2)
	releases := make([]func(), 0, autoProbeConcurrency)
	for index := 0; index < autoProbeConcurrency; index++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("reserve concurrent pair: %v", result.err)
		}
		releases = append(releases, result.release)
		for _, port := range []int{result.socks, result.http} {
			if _, duplicate := seen[port]; duplicate {
				t.Fatalf("port %d was reserved twice", port)
			}
			seen[port] = struct{}{}
		}
	}
	for _, release := range releases {
		release()
	}
	assertOnlyURLTestGuardRemains(t, root)
}

func TestURLTestPortLockHelper(t *testing.T) {
	if os.Getenv("FASTLANE_PORT_LOCK_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	root := os.Getenv("FASTLANE_PORT_LOCK_ROOT")
	rawPorts := strings.Split(os.Getenv("FASTLANE_PORT_LOCK_PORTS"), ",")
	ports := make([]int, 0, len(rawPorts))
	for _, rawPort := range rawPorts {
		port, err := strconv.Atoi(rawPort)
		if err != nil {
			t.Fatalf("parse helper port %q: %v", rawPort, err)
		}
		ports = append(ports, port)
	}
	locks, acquired, err := acquireURLTestPortLocksAt(root, ports...)
	if err != nil {
		t.Fatalf("acquire helper locks: %v", err)
	}
	wantAcquired := os.Getenv("FASTLANE_PORT_LOCK_EXPECT") == "acquired"
	if acquired != wantAcquired {
		t.Fatalf("acquired=%t, want %t", acquired, wantAcquired)
	}
	if acquired {
		releaseURLTestPortLocks(root, locks)
	}
}

func runPortLockHelper(t *testing.T, root string, ports []int, wantAcquired bool) {
	t.Helper()
	rawPorts := make([]string, 0, len(ports))
	for _, port := range ports {
		rawPorts = append(rawPorts, strconv.Itoa(port))
	}
	expectation := "busy"
	if wantAcquired {
		expectation = "acquired"
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestURLTestPortLockHelper$")
	cmd.Env = append(os.Environ(),
		"FASTLANE_PORT_LOCK_HELPER=1",
		"FASTLANE_PORT_LOCK_ROOT="+root,
		"FASTLANE_PORT_LOCK_PORTS="+strings.Join(rawPorts, ","),
		"FASTLANE_PORT_LOCK_EXPECT="+expectation,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("port lock helper (%s): %v\n%s", expectation, err, output)
	}
}

func assertOnlyURLTestGuardRemains(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read lock root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != urlTestPortGuardName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("unexpected persistent lock files: %v", names)
	}
}
