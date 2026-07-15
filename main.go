package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/vishvananda/netlink"
)

const raInterval = 30 * time.Second

type lanSetup struct {
	targetNet *net.IPNet
	gatewayIP net.IP
}

func main() {
	log.SetFlags(0)

	cfg, err := loadRuntimeConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	printConfig(cfg)

	setup, lanLink, err := initializeLAN(cfg)
	if err != nil {
		log.Fatalf("Error initializing LAN: %v", err)
	}
	defer stopDnsmasq()

	raSender, err := startRouterAdvertisements(cfg.LanInterface, lanLink)
	if err != nil {
		log.Fatalf("Error starting router advertisements: %v", err)
	}
	defer raSender.Close()

	fmt.Printf("Listening on LAN (%s)...\n", cfg.LanInterface)

	state := loadNeighborState(cfg.WanInterface, cfg.LanInterface)
	watchLANNeighbors(
		lanLink,
		setup.targetNet,
		setup.gatewayIP,
		cfg.LanInterface,
		cfg.WanInterface,
		&state,
		raSender,
	)
}

func printConfig(cfg *runtimeConfig) {
	fmt.Printf("Config loaded | WAN: %s | LAN: %s | LAN Subnet: /%d | LAN IP: %s\n",
		cfg.WanInterface, cfg.LanInterface, cfg.LanSubnetSize, cfg.LanIPPosition)
	fmt.Printf("ULA address: %s | ULA DHCP range: %s\n", cfg.LanULAAddress, cfg.ULADHCPRange)
}

func initializeLAN(cfg *runtimeConfig) (*lanSetup, netlink.Link, error) {
	wanIP, _, err := getGlobalIPv6(cfg.WanInterface)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to detect global IPv6 on %s: %w", cfg.WanInterface, err)
	}
	fmt.Printf("WAN IPv6 detected: %s\n", wanIP)

	targetNet, gatewayIP, err := calculateLANNetworkAndIP(wanIP, cfg.LanSubnetSize, cfg.LanIPPosition)
	if err != nil {
		return nil, nil, fmt.Errorf("calculating LAN network: %w", err)
	}
	fmt.Printf("Target LAN prefix: %s\n", targetNet.String())
	fmt.Printf("Calculated LAN gateway IP: %s/%d\n", gatewayIP.String(), cfg.LanSubnetSize)

	if err := configureLanInterface(cfg.LanInterface, gatewayIP, cfg.LanSubnetSize, cfg.LanULAAddress); err != nil {
		return nil, nil, fmt.Errorf("configuring LAN interface %s: %w", cfg.LanInterface, err)
	}

	if err := applyNetworkSysctl(cfg.WanInterface, cfg.LanInterface); err != nil {
		return nil, nil, fmt.Errorf("applying network sysctl: %w", err)
	}

	prefixDHCPRange := prefixDHCPRange(wanIP, cfg.LanSubnetSize)
	if err := startDnsmasq(cfg.LanInterface, cfg.ULADHCPRange, prefixDHCPRange); err != nil {
		return nil, nil, fmt.Errorf("starting dnsmasq: %w", err)
	}
	logDnsmasqStarted()

	lanLink, err := netlink.LinkByName(cfg.LanInterface)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to find LAN interface %s: %w", cfg.LanInterface, err)
	}

	return &lanSetup{
		targetNet: targetNet,
		gatewayIP: gatewayIP,
	}, lanLink, nil
}

func startRouterAdvertisements(lanIf string, lanLink netlink.Link) (*raSender, error) {
	macSrc := lanLink.Attrs().HardwareAddr
	if !isValidMAC(macSrc) {
		return nil, fmt.Errorf("invalid MAC address on %s", lanIf)
	}

	srcIP := raSourceAddress()
	if srcIP == nil {
		return nil, fmt.Errorf("invalid RA source address %s", raLinkLocalSource)
	}

	raSender, err := newRASender(lanIf, macSrc, srcIP, raULANetwork())
	if err != nil {
		return nil, fmt.Errorf("initializing RA sender on %s: %w", lanIf, err)
	}

	go raSender.StartPeriodic(raInterval)
	return raSender, nil
}
