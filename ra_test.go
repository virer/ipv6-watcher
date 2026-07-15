package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func TestRAICMPPayloadLayout(t *testing.T) {
	mac := net.HardwareAddr{0x42, 0x94, 0x8f, 0x08, 0x67, 0xff}
	prefix := raULANetwork()
	srcIP := raSourceAddress()

	ip6 := &layers.IPv6{
		Version:    6,
		NextHeader: layers.IPProtocolICMPv6,
		HopLimit:   255,
		SrcIP:      srcIP,
		DstIP:      net.ParseIP("ff02::1"),
	}

	ra := &layers.ICMPv6RouterAdvertisement{
		HopLimit:       64,
		Flags:          raFlags,
		RouterLifetime: 1800,
		Options:        buildRAOptions(mac, prefix),
	}

	icmp6 := &layers.ICMPv6{
		TypeCode: layers.CreateICMPv6TypeCode(layers.ICMPv6TypeRouterAdvertisement, 0),
	}
	if err := icmp6.SetNetworkLayerForChecksum(ip6); err != nil {
		t.Fatalf("checksum setup: %v", err)
	}

	buffer := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}, ip6, icmp6, ra); err != nil {
		t.Fatalf("serialization: %v", err)
	}

	packet := buffer.Bytes()
	if len(packet) < 40+56 {
		t.Fatalf("packet too short: %d bytes", len(packet))
	}

	payload := packet[40:]
	if len(payload) != 56 {
		t.Fatalf("ICMP payload length = %d, want 56", len(payload))
	}

	if payload[0] != 134 || payload[1] != 0 {
		t.Fatalf("ICMP type/code = %d/%d, want 134/0", payload[0], payload[1])
	}

	if payload[4] != 64 || payload[5] != raFlags {
		t.Fatalf("RA hop limit/flags = %d/0x%02x, want 64/0x%02x", payload[4], payload[5], raFlags)
	}

	if binary.BigEndian.Uint16(payload[6:8]) != 1800 {
		t.Fatalf("router lifetime = %d, want 1800", binary.BigEndian.Uint16(payload[6:8]))
	}

	if payload[16] != 1 || payload[17] != 1 {
		t.Fatalf("SLLA option = %d/%d, want 1/1", payload[16], payload[17])
	}
	if !bytes.Equal(payload[18:24], []byte(mac)) {
		t.Fatalf("SLLA MAC = %x, want %s", payload[18:24], mac)
	}

	if payload[24] != 3 || payload[25] != 4 {
		t.Fatalf("prefix option = %d/%d, want 3/4", payload[24], payload[25])
	}
	if payload[26] != 64 || payload[27] != raPrefixFlags {
		t.Fatalf("prefix length/flags = %d/0x%02x, want 64/0x%02x", payload[26], payload[27], raPrefixFlags)
	}
	if binary.BigEndian.Uint32(payload[28:32]) != raValidLifetime {
		t.Fatalf("valid lifetime = %d, want %d", binary.BigEndian.Uint32(payload[28:32]), raValidLifetime)
	}
	if binary.BigEndian.Uint32(payload[32:36]) != raPreferredLifetime {
		t.Fatalf("preferred lifetime = %d, want %d", binary.BigEndian.Uint32(payload[32:36]), raPreferredLifetime)
	}

	wantPrefix := net.ParseIP(raULAPrefixNetwork)
	if got := net.IP(payload[40:56]); !got.Equal(wantPrefix) {
		t.Fatalf("prefix = %s, want %s", got, wantPrefix)
	}
}
