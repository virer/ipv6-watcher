package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const dnsmasqPIDFile = "/var/run/dnsmasq-dhcpv6.pid"

var dnsmasqCmd *exec.Cmd

func startDnsmasq(lanIf, ulaDHCPRange, prefixDHCPRange string) error {
	dnsmasqCmd = exec.Command("dnsmasq",
		"--keep-in-foreground",
		"--port=0",
		"--interface="+lanIf,
		"--bind-interfaces",
		"--pid-file="+dnsmasqPIDFile,
		"--dhcp-hostsfile=/var/run/dnsmasq-dhcpv6.hosts",
		"--dhcp-leasefile=/var/run/dnsmasq-dhcpv6.leases",
		"--dhcp-range="+ulaDHCPRange,
		"--dhcp-range="+prefixDHCPRange,
	)
	dnsmasqCmd.Stdout = os.Stdout
	dnsmasqCmd.Stderr = os.Stderr

	if err := dnsmasqCmd.Start(); err != nil {
		dnsmasqCmd = nil
		return err
	}

	fmt.Printf("dnsmasq ula dhcp-range: %s\n", ulaDHCPRange)
	fmt.Printf("dnsmasq prefix dhcp-range: %s\n", prefixDHCPRange)
	return nil
}

func readDnsmasqPID() (int, error) {
	data, err := os.ReadFile(dnsmasqPIDFile)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %s", dnsmasqPIDFile)
	}

	return pid, nil
}

func logDnsmasqStarted() {
	if pid, err := readDnsmasqPID(); err == nil {
		fmt.Printf("dnsmasq started (pid %d)\n", pid)
		return
	}
	if dnsmasqCmd != nil && dnsmasqCmd.Process != nil {
		fmt.Printf("dnsmasq started (pid %d)\n", dnsmasqCmd.Process.Pid)
		return
	}
	fmt.Printf("dnsmasq started\n")
}

func stopDnsmasq() {
	if dnsmasqCmd != nil && dnsmasqCmd.Process != nil {
		pid := dnsmasqCmd.Process.Pid
		if err := dnsmasqCmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Error sending SIGTERM to dnsmasq (pid %d): %v", pid, err)
		}

		waitDone := make(chan struct{})
		go func() {
			_ = dnsmasqCmd.Wait()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			fmt.Printf("dnsmasq stopped (pid %d)\n", pid)
		case <-time.After(3 * time.Second):
			if err := dnsmasqCmd.Process.Kill(); err != nil {
				log.Printf("Error killing dnsmasq (pid %d): %v", pid, err)
			} else {
				<-waitDone
				fmt.Printf("dnsmasq killed (pid %d)\n", pid)
			}
		}

		dnsmasqCmd = nil
		_ = os.Remove(dnsmasqPIDFile)
		return
	}

	pid, err := readDnsmasqPID()
	if err != nil {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	_ = proc.Kill()
	_ = os.Remove(dnsmasqPIDFile)
}
