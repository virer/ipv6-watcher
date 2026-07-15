package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

const (
	defaultConfigFile    = "config.json"
	defaultLanULAAddress = "fd00:1111:cafe::1"
	defaultULADHCPRange  = "fd00:1111:cafe::10,fd00:1111:cafe::ffff:fff6,64,12h"
	lanULAPrefixSize     = 64
)

type Config struct {
	WanInterface  string `json:"wan_interface"`
	LanInterface  string `json:"lan_interface"`
	LanSubnetSize int    `json:"lan_subnet_size"`
	LanIPPosition string `json:"lan_ip_position"` // "first" or "last"
	LanULAAddress string `json:"lan_ula_address"`
	ULADHCPRange  string `json:"ula_dhcp_range"`
}

type runtimeConfig struct {
	*Config
	configFile string
}

func loadRuntimeConfig() (*runtimeConfig, error) {
	configFile := flag.String("config", defaultConfigFile, "path to configuration file")
	ulaAddress := flag.String("ula-address", "", "ULA address on the LAN interface")
	ulaDHCPRange := flag.String("ula-dhcp-range", "", "dnsmasq DHCPv6 range for the ULA prefix")
	flag.Parse()

	config, err := loadConfig(*configFile)
	if err != nil {
		return nil, err
	}

	if *ulaAddress != "" {
		config.LanULAAddress = *ulaAddress
	}
	if *ulaDHCPRange != "" {
		config.ULADHCPRange = *ulaDHCPRange
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return &runtimeConfig{
		Config:     config,
		configFile: *configFile,
	}, nil
}

func loadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(bytes, &config); err != nil {
		return nil, err
	}

	config.applyDefaults()
	return &config, nil
}

func (c *Config) applyDefaults() {
	c.LanIPPosition = strings.ToLower(strings.TrimSpace(c.LanIPPosition))
	if c.LanIPPosition == "" {
		c.LanIPPosition = "first"
	}
	if c.LanULAAddress == "" {
		c.LanULAAddress = defaultLanULAAddress
	}
	if c.ULADHCPRange == "" {
		c.ULADHCPRange = defaultULADHCPRange
	}
}

func (c *Config) validate() error {
	if c.LanIPPosition != "first" && c.LanIPPosition != "last" {
		return fmt.Errorf("invalid lan_ip_position (%s): only 'first' and 'last' are accepted", c.LanIPPosition)
	}
	if net.ParseIP(c.LanULAAddress) == nil {
		return fmt.Errorf("invalid lan_ula_address (%s)", c.LanULAAddress)
	}
	if strings.TrimSpace(c.ULADHCPRange) == "" {
		return fmt.Errorf("ula_dhcp_range must not be empty")
	}
	return nil
}
