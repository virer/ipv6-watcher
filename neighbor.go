package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

type neighborState struct {
	proxied    map[string]struct{}
	routed     map[string]struct{}
	neighbored map[string]struct{}
}

func loadNeighborState(wanIf, lanIf string) neighborState {
	state := neighborState{
		proxied:    loadProxiedIPs(wanIf),
		routed:     loadClientRoutes(lanIf),
		neighbored: loadNeighboredIPs(lanIf),
	}

	if len(state.proxied) > 0 {
		fmt.Printf("Loaded %d existing NDP proxy entries\n", len(state.proxied))
	}
	if len(state.routed) > 0 {
		fmt.Printf("Loaded %d existing client routes\n", len(state.routed))
	}
	if len(state.neighbored) > 0 {
		fmt.Printf("Loaded %d existing neighbor entries\n", len(state.neighbored))
	}

	return state
}

func watchLANNeighbors(
	lanLink netlink.Link,
	targetNet *net.IPNet,
	gatewayIP net.IP,
	lanIf, wanIf string,
	state *neighborState,
	raSender *raSender,
) {
	updates := make(chan netlink.NeighUpdate)
	done := make(chan struct{})
	defer close(done)

	if err := netlink.NeighSubscribe(updates, done); err != nil {
		log.Fatalf("Netlink subscription error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-sigCh:
			fmt.Println("Shutting down...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}

			if update.Neigh.LinkIndex != lanLink.Attrs().Index || update.Neigh.IP.To4() != nil {
				continue
			}

			clientIP := update.Neigh.IP
			if !targetNet.Contains(clientIP) || clientIP.Equal(gatewayIP) {
				continue
			}

			handleClientDetection(
				clientIP,
				update.Neigh,
				lanIf,
				wanIf,
				lanLink,
				state.proxied,
				state.routed,
				state.neighbored,
				raSender,
			)
		}
	}
}

func loadProxiedIPs(wanIf string) map[string]struct{} {
	proxied := make(map[string]struct{})

	out, err := exec.Command("ip", "-6", "neigh", "show", "proxy", "dev", wanIf).Output()
	if err != nil {
		return proxied
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			proxied[fields[0]] = struct{}{}
		}
	}

	return proxied
}

func addNdpProxy(ip string, wanIf string, proxied map[string]struct{}) {
	if _, ok := proxied[ip]; ok {
		return
	}

	cmd := exec.Command("ip", "-6", "neigh", "add", "proxy", ip, "dev", wanIf)
	err := cmd.Run()
	if err != nil {
		if strings.Contains(err.Error(), "exit status 2") {
			proxied[ip] = struct{}{}
			return
		}
		log.Printf("Error proxying %s: %v", ip, err)
		return
	}

	proxied[ip] = struct{}{}
	fmt.Printf("NDP proxy configured on %s for %s\n", wanIf, ip)
}

func loadClientRoutes(lanIf string) map[string]struct{} {
	routed := make(map[string]struct{})

	out, err := exec.Command("ip", "-6", "route", "show", "dev", lanIf).Output()
	if err != nil {
		return routed
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.Contains(fields[0], ":") {
			continue
		}
		if strings.Contains(fields[0], "/") {
			continue
		}
		routed[fields[0]] = struct{}{}
	}

	return routed
}

func addClientRoute(ip, lanIf string, routed map[string]struct{}) {
	if _, ok := routed[ip]; ok {
		return
	}

	cmd := exec.Command("ip", "-6", "route", "add", ip, "dev", lanIf)
	err := cmd.Run()
	if err != nil {
		if strings.Contains(err.Error(), "File exists") || strings.Contains(err.Error(), "exit status 2") {
			routed[ip] = struct{}{}
			return
		}
		log.Printf("Error adding route for %s: %v", ip, err)
		return
	}

	routed[ip] = struct{}{}
	fmt.Printf("Route added for %s via %s\n", ip, lanIf)
}

func isValidMAC(mac net.HardwareAddr) bool {
	if len(mac) == 0 {
		return false
	}
	for _, b := range mac {
		if b != 0 {
			return true
		}
	}
	return false
}

func resolveClientMAC(lanIf string, link netlink.Link, clientIP net.IP) (net.HardwareAddr, error) {
	linkIndex := link.Attrs().Index

	for attempt := 0; attempt < 3; attempt++ {
		_ = exec.Command("ping", "-6", "-c", "1", "-W", "2", "-I", lanIf, clientIP.String()).Run()

		neighs, err := netlink.NeighList(linkIndex, netlink.FAMILY_V6)
		if err == nil {
			for _, neigh := range neighs {
				if neigh.IP == nil || !neigh.IP.Equal(clientIP) || !isValidMAC(neigh.HardwareAddr) {
					continue
				}
				if neigh.State&netlink.NUD_FAILED != 0 {
					continue
				}
				return neigh.HardwareAddr, nil
			}
		}

		if attempt < 2 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil, fmt.Errorf("MAC not resolved for %s", clientIP)
}

func handleClientDetection(
	clientIP net.IP,
	neigh netlink.Neigh,
	lanIf, wanIf string,
	lanLink netlink.Link,
	proxied, routed, neighbored map[string]struct{},
	raSender *raSender,
) {
	ipStr := clientIP.String()
	isNewClient := false
	if _, ok := routed[ipStr]; !ok {
		isNewClient = true
	}

	addClientRoute(ipStr, lanIf, routed)
	addNdpProxy(ipStr, wanIf, proxied)

	if isNewClient {
		raSender.send("new-client")
	}

	mac := neigh.HardwareAddr
	if !isValidMAC(mac) {
		mac, _ = resolveClientMAC(lanIf, lanLink, clientIP)
	}
	if !isValidMAC(mac) {
		fmt.Printf("Host detected on LAN: %s (MAC: unresolved)\n", clientIP)
		return
	}

	fmt.Printf("Host detected on LAN: %s (MAC: %s)\n", clientIP, mac)
	addClientNeighbor(lanLink.Attrs().Index, clientIP, mac, neighbored)
}

func loadNeighboredIPs(lanIf string) map[string]struct{} {
	neighbored := make(map[string]struct{})

	out, err := exec.Command("ip", "-6", "neigh", "show", "dev", lanIf).Output()
	if err != nil {
		return neighbored
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "INCOMPLETE") || !strings.Contains(line, "lladdr") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.Contains(fields[0], ":") {
			neighbored[fields[0]] = struct{}{}
		}
	}

	return neighbored
}

func addClientNeighbor(linkIndex int, ip net.IP, mac net.HardwareAddr, neighbored map[string]struct{}) {
	ipStr := ip.String()
	if _, ok := neighbored[ipStr]; ok {
		return
	}

	err := netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    linkIndex,
		Family:       netlink.FAMILY_V6,
		IP:           ip,
		HardwareAddr: mac,
		State:        netlink.NUD_REACHABLE,
	})
	if err != nil {
		log.Printf("Error adding neighbor entry for %s: %v", ipStr, err)
		return
	}

	neighbored[ipStr] = struct{}{}
	fmt.Printf("Neighbor entry added for %s (%s)\n", ipStr, mac)
}
