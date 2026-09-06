package speedtest

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processOwnsTCPListenerAt verifies listener ownership from Linux procfs. It is
// kept independent of the host OS so its parser can be tested deterministically.
func processOwnsTCPListenerAt(procRoot string, pid int, address string) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	_, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return false, fmt.Errorf("parse listener address %q: %w", address, err)
	}
	portValue, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil || portValue == 0 {
		return false, fmt.Errorf("parse listener port %q", rawPort)
	}

	processRoot := filepath.Join(procRoot, strconv.Itoa(pid))
	fdEntries, err := os.ReadDir(filepath.Join(processRoot, "fd"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read process descriptors: %w", err)
	}
	ownedSockets := make(map[string]struct{}, len(fdEntries))
	for _, entry := range fdEntries {
		target, err := os.Readlink(filepath.Join(processRoot, "fd", entry.Name()))
		if err != nil {
			// Descriptors can disappear while procfs is being inspected.
			continue
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			ownedSockets[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = struct{}{}
		}
	}
	if len(ownedSockets) == 0 {
		return false, nil
	}

	foundTable := false
	for _, tableName := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile(filepath.Join(processRoot, "net", tableName))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("read process %s sockets: %w", tableName, err)
		}
		foundTable = true
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" { // TCP_LISTEN
				continue
			}
			localParts := strings.Split(fields[1], ":")
			if len(localParts) < 2 {
				continue
			}
			port, err := strconv.ParseUint(localParts[len(localParts)-1], 16, 16)
			if err != nil || port != portValue {
				continue
			}
			if _, owned := ownedSockets[fields[9]]; owned {
				return true, nil
			}
		}
	}
	if !foundTable {
		return false, fmt.Errorf("process TCP socket tables are unavailable")
	}
	return false, nil
}
