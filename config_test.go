package main

import "testing"

func TestConfigValidateQuietVerbose(t *testing.T) {
	cfg := &Config{
		LanIPPosition: "first",
		LanULAAddress: defaultLanULAAddress,
		ULADHCPRange:  defaultULADHCPRange,
		Quiet:         true,
		Verbose:       true,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error when quiet and verbose are both enabled")
	}
}

func TestConfigValidateVerbose(t *testing.T) {
	cfg := &Config{
		LanIPPosition: "first",
		LanULAAddress: defaultLanULAAddress,
		ULADHCPRange:  defaultULADHCPRange,
		Verbose:       true,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
