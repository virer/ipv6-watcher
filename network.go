package main

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"
)

func getGlobalIPv6(interfaceName string) (net.IP, int, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, 0, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, 0, err
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() == nil && !ipNet.IP.IsLinkLocalUnicast() {
				prefixSize, _ := ipNet.Mask.Size()
				return ipNet.IP, prefixSize, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("no global IPv6 address configured")
}

func calculateLANNetworkAndIP(ip net.IP, size int, position string) (*net.IPNet, net.IP, error) {
	bytesCount := size / 8
	mask := net.CIDRMask(size, 128)

	networkIP := make(net.IP, 16)
	copy(networkIP[:bytesCount], ip[:bytesCount])

	lanIP := make(net.IP, 16)
	copy(lanIP, networkIP)

	switch position {
	case "first":
		lanIP[15] = 1
	case "last":
		for i := bytesCount; i < 16; i++ {
			lanIP[i] = 0xff
		}
	}

	return &net.IPNet{
		IP:   networkIP,
		Mask: mask,
	}, lanIP, nil
}

const lanRouterLinkLocal = "fe80::1"

func configureLanInterface(lanIf string, ip net.IP, size int, ulaAddress string) error {
	link, err := netlink.LinkByName(lanIf)
	if err != nil {
		return err
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err == nil {
		for _, addr := range addrs {
			if !addr.IP.IsLinkLocalUnicast() {
				_ = netlink.AddrDel(link, &addr)
			}
		}
	}

	gatewayAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(size, 128),
		},
	}
	if err := netlink.AddrAdd(link, gatewayAddr); err != nil && !strings.Contains(err.Error(), "file exists") {
		return err
	}

	routerLinkLocal := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(lanRouterLinkLocal),
			Mask: net.CIDRMask(64, 128),
		},
	}
	if err := netlink.AddrAdd(link, routerLinkLocal); err != nil && !strings.Contains(err.Error(), "file exists") {
		return err
	}

	ulaAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(ulaAddress),
			Mask: net.CIDRMask(lanULAPrefixSize, 128),
		},
	}
	if err := netlink.AddrAdd(link, ulaAddr); err != nil && !strings.Contains(err.Error(), "file exists") {
		return err
	}

	return netlink.LinkSetUp(link)
}

func applyNetworkSysctl(wanIf, lanIf string) error {
	settings := []string{
		fmt.Sprintf("net.ipv6.conf.%s.proxy_ndp=1", wanIf),
		fmt.Sprintf("net.ipv6.neigh.%s.base_reachable_time=3600", lanIf),
	}

	for _, setting := range settings {
		cmd := exec.Command("sysctl", "-w", setting)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("sysctl -w %s: %w (%s)", setting, err, strings.TrimSpace(string(out)))
		}
		infof("sysctl -w %s\n", setting)
	}

	return nil
}

func prefixDHCPRange(wanIP net.IP, prefixSize int) string {
	networkIP := networkBase(wanIP, prefixSize)
	start, end := dhcpRangeAddresses(networkIP, prefixSize)
	return fmt.Sprintf("%s,%s,%d,12h", start, end, prefixSize)
}

func networkBase(ip net.IP, prefixSize int) net.IP {
	networkIP := make(net.IP, 16)
	copy(networkIP[:prefixSize/8], ip[:prefixSize/8])
	return networkIP
}

func dhcpRangeAddresses(networkIP net.IP, prefixSize int) (net.IP, net.IP) {
	start := make(net.IP, 16)
	end := make(net.IP, 16)
	copy(start, networkIP)
	copy(end, networkIP)

	bytesCount := prefixSize / 8
	start[15] = 0x10

	for i := bytesCount; i < 14; i++ {
		end[i] = 0xff
	}
	end[15] = 0xf6

	return start, end
}
