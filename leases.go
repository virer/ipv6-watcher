package main

import (
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const leasePollInterval = 2 * time.Second

// parseDnsmasqLeases extracts IPv6 addresses from dnsmasq DHCPv6 lease file content.
// Lease lines look like: <expiry> <iaid> <address> <hostname> <client-id>
// The leading "duid ..." line is ignored.
func parseDnsmasqLeases(content string) []net.IP {
	var ips []net.IP
	seen := make(map[string]struct{})

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "duid ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		ip := net.ParseIP(fields[2])
		if ip == nil || ip.To4() != nil {
			continue
		}

		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, ip)
	}

	return ips
}

func readDnsmasqLeaseIPs(path string) ([]net.IP, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseDnsmasqLeases(string(data)), nil
}

// watchDnsmasqLeases polls the lease file and sends IPv6 addresses that fall inside
// targetNet whenever the file changes (including the initial read).
func watchDnsmasqLeases(path string, targetNet *net.IPNet, out chan<- string, done <-chan struct{}) {
	var lastMod time.Time
	var lastSize int64
	haveStat := false

	emit := func() {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if haveStat && info.ModTime().Equal(lastMod) && info.Size() == lastSize {
			return
		}
		lastMod = info.ModTime()
		lastSize = info.Size()
		haveStat = true

		ips, err := readDnsmasqLeaseIPs(path)
		if err != nil {
			log.Printf("Error reading dnsmasq leases: %v", err)
			return
		}

		for _, ip := range ips {
			if targetNet != nil && !targetNet.Contains(ip) {
				continue
			}
			ipStr := ip.String()
			select {
			case out <- ipStr:
			case <-done:
				return
			}
		}
	}

	emit()

	ticker := time.NewTicker(leasePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			emit()
		}
	}
}
