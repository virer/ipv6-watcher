package main

import (
	"net"
	"testing"
)

func TestParseDnsmasqLeases(t *testing.T) {
	content := `duid 00:01:00:01:31:eb:3d:bb:3e:94:8f:08:67:ff
1784256021 1448103320 fd00:1111:cafe::a53e:7d96 * 00:04:c9:fb:29:a4:1c:38:1c:70:60:91:8d:e2:c3:26:b4:dc
1784256021 1448103320 2a02:2788:854:419:3c94:8fff:65e:39cc * 00:04:c9:fb:29:a4:1c:38:1c:70:60:91:8d:e2:c3:26:b4:dc
1784237835 1448103320 fd00:1111:cafe::1501:f59e ctf26 00:04:43:31:1c:f4:5a:51:04:18:77:27:71:e5:69:5e:bd:3d
1784237835 1448103320 2a02:2788:854:419:3c94:8fff:1d88:207e ctf26 00:04:43:31:1c:f4:5a:51:04:18:77:27:71:e5:69:5e:bd:3d
`

	ips := parseDnsmasqLeases(content)
	want := []string{
		"fd00:1111:cafe::a53e:7d96",
		"2a02:2788:854:419:3c94:8fff:65e:39cc",
		"fd00:1111:cafe::1501:f59e",
		"2a02:2788:854:419:3c94:8fff:1d88:207e",
	}
	if len(ips) != len(want) {
		t.Fatalf("got %d ips, want %d: %v", len(ips), len(want), ips)
	}
	for i, ip := range ips {
		if ip.String() != want[i] {
			t.Errorf("ip[%d] = %s, want %s", i, ip, want[i])
		}
	}
}

func TestParseDnsmasqLeasesSkipsInvalid(t *testing.T) {
	content := `
duid 00:01:00:01:aa:bb
not-enough-fields
1784256021 1448103320 not-an-ip * 00:04:aa
1784256021 1448103320 192.168.1.10 * 00:04:aa
1784256021 1448103320 2001:db8::1 host 00:04:aa
1784256021 1448103320 2001:db8::1 host 00:04:aa
`
	ips := parseDnsmasqLeases(content)
	if len(ips) != 1 || ips[0].String() != "2001:db8::1" {
		t.Fatalf("got %v, want [2001:db8::1]", ips)
	}
}

func TestParseDnsmasqLeasesEmpty(t *testing.T) {
	if ips := parseDnsmasqLeases(""); len(ips) != 0 {
		t.Fatalf("got %v, want empty", ips)
	}
}

func TestWatchFilterTargetNet(t *testing.T) {
	_, targetNet, err := net.ParseCIDR("2a02:2788:854:419::/64")
	if err != nil {
		t.Fatal(err)
	}

	content := `1784256021 1 fd00:1111:cafe::1 * 00:04:aa
1784256021 1 2a02:2788:854:419:3c94:8fff:65e:39cc * 00:04:aa
`
	var matched []string
	for _, ip := range parseDnsmasqLeases(content) {
		if targetNet.Contains(ip) {
			matched = append(matched, ip.String())
		}
	}
	if len(matched) != 1 || matched[0] != "2a02:2788:854:419:3c94:8fff:65e:39cc" {
		t.Fatalf("got %v", matched)
	}
}
