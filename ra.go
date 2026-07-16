package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

const (
	raLinkLocalSource    = "fe80::1"
	raULAPrefixNetwork   = "fd00:1111:cafe::"
	raULAPrefixSize      = 64
	raValidLifetime      = 86400
	raPreferredLifetime  = 14400
	raFlags              = 0xC0 // M=1 (managed), O=1 (other) -> use DHCPv6
	raPrefixFlags        = 0x80 // L=1 (on-link), A=0 (no SLAAC; DHCPv6 assigns addresses)
)

type raSender struct {
	conn    *os.File
	lanIf   string
	srcIP   net.IP
	macSrc  net.HardwareAddr
	prefix  *net.IPNet
	verbose bool
}

func newRASender(lanIf string, macSrc net.HardwareAddr, srcIP net.IP, prefix *net.IPNet) (*raSender, error) {
	if srcIP == nil {
		return nil, fmt.Errorf("invalid RA source address")
	}
	if prefix == nil {
		return nil, fmt.Errorf("invalid RA prefix")
	}

	iface, err := net.InterfaceByName(lanIf)
	if err != nil {
		return nil, err
	}

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ALL)))
	if err != nil {
		return nil, fmt.Errorf("open AF_PACKET socket: %w", err)
	}

	addr := syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_IPV6),
		Ifindex:  iface.Index,
	}
	if err := syscall.Bind(fd, &addr); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("bind AF_PACKET to %s: %w", lanIf, err)
	}

	return &raSender{
		conn:   os.NewFile(uintptr(fd), "ra-packet"),
		lanIf:  lanIf,
		srcIP:  srcIP,
		macSrc: macSrc,
		prefix: prefix,
	}, nil
}

func (s *raSender) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *raSender) StartPeriodic(interval time.Duration) {
	s.send("periodic")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		s.send("periodic")
	}
}

func (s *raSender) send(trigger string) {
	if err := s.sendRA(); err != nil {
		log.Printf("Error sending RA on %s (%s): %v", s.lanIf, trigger, err)
		return
	}
	if s.verbose {
		log.Printf("Router advertisement sent on %s (%s, src %s, prefix %s)", s.lanIf, trigger, s.srcIP, s.prefix)
	}
}

func (s *raSender) sendRA() error {
	ip6 := &layers.IPv6{
		Version:    6,
		NextHeader: layers.IPProtocolICMPv6,
		HopLimit:   255,
		SrcIP:      s.srcIP,
		DstIP:      net.ParseIP("ff02::1"),
	}

	ra := &layers.ICMPv6RouterAdvertisement{
		HopLimit:       64,
		Flags:          raFlags,
		RouterLifetime: 1800,
		Options:        buildRAOptions(s.macSrc, s.prefix),
	}

	icmp6 := &layers.ICMPv6{
		TypeCode: layers.CreateICMPv6TypeCode(layers.ICMPv6TypeRouterAdvertisement, 0),
	}
	if err := icmp6.SetNetworkLayerForChecksum(ip6); err != nil {
		return fmt.Errorf("checksum setup: %w", err)
	}

	eth := &layers.Ethernet{
		SrcMAC:       s.macSrc,
		DstMAC:       allNodesMulticastMAC,
		EthernetType: layers.EthernetTypeIPv6,
	}

	buffer := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}

	if err := gopacket.SerializeLayers(buffer, options, eth, ip6, icmp6, ra); err != nil {
		return fmt.Errorf("serialization: %w", err)
	}

	if _, err := s.conn.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("packet send: %w", err)
	}

	return nil
}

func buildRAOptions(macSrc net.HardwareAddr, prefix *net.IPNet) layers.ICMPv6Options {
	var opts layers.ICMPv6Options

	if prefix != nil {
		prefixData := make([]byte, 30)
		prefixData[0] = raULAPrefixSize
		prefixData[1] = raPrefixFlags
		binary.BigEndian.PutUint32(prefixData[2:6], raValidLifetime)
		binary.BigEndian.PutUint32(prefixData[6:10], raPreferredLifetime)
		copy(prefixData[14:], prefix.IP)

		opts = append(opts, layers.ICMPv6Option{
			Type: layers.ICMPv6OptPrefixInfo,
			Data: prefixData,
		})
	}

	// gopacket prepends options; SLLA must be listed last to appear first on the wire.
	opts = append(opts, layers.ICMPv6Option{
		Type: layers.ICMPv6OptSourceAddress,
		Data: []byte(macSrc),
	})

	return opts
}

func raSourceAddress() net.IP {
	return net.ParseIP(raLinkLocalSource)
}

func raULANetwork() *net.IPNet {
	return &net.IPNet{
		IP:   net.ParseIP(raULAPrefixNetwork),
		Mask: net.CIDRMask(raULAPrefixSize, 128),
	}
}

var allNodesMulticastMAC = net.HardwareAddr{0x33, 0x33, 0x00, 0x00, 0x00, 0x01}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | v>>8
}
