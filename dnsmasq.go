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

const (
	dnsmasqPIDFile   = "/var/run/dnsmasq-dhcpv6.pid"
	dnsmasqLeaseFile = "/var/run/dnsmasq-dhcpv6.leases"
)

var dnsmasqCmd *exec.Cmd

func startDnsmasq(lanIf, ulaDHCPRange, prefixDHCPRange string, quiet, verbose bool) error {
	args := []string{
		"--keep-in-foreground",
		"--port=0",
		"--interface=" + lanIf,
		"--bind-dynamic",
		"--pid-file=" + dnsmasqPIDFile,
		"--dhcp-hostsfile=/var/run/dnsmasq-dhcpv6.hosts",
		"--dhcp-leasefile=" + dnsmasqLeaseFile,
		"--dhcp-range=" + ulaDHCPRange,
		"--dhcp-range=" + prefixDHCPRange,
	}
	if quiet {
		args = append(args, "--quiet-dhcp6")
	}
	if verbose {
		args = append(args, "--log-dhcp")
	}

	dnsmasqCmd = exec.Command("dnsmasq", args...)
	dnsmasqCmd.Stdout = os.Stdout
	dnsmasqCmd.Stderr = os.Stderr

	if err := dnsmasqCmd.Start(); err != nil {
		dnsmasqCmd = nil
		return err
	}

	infof("dnsmasq ula dhcp-range: %s\n", ulaDHCPRange)
	infof("dnsmasq prefix dhcp-range: %s\n", prefixDHCPRange)
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
		infof("dnsmasq started (pid %d)\n", pid)
		return
	}
	if dnsmasqCmd != nil && dnsmasqCmd.Process != nil {
		infof("dnsmasq started (pid %d)\n", dnsmasqCmd.Process.Pid)
		return
	}
	infof("dnsmasq started\n")
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
			infof("dnsmasq stopped (pid %d)\n", pid)
		case <-time.After(3 * time.Second):
			if err := dnsmasqCmd.Process.Kill(); err != nil {
				log.Printf("Error killing dnsmasq (pid %d): %v", pid, err)
			} else {
				<-waitDone
				infof("dnsmasq killed (pid %d)\n", pid)
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
