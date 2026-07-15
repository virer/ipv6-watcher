# ipv6-watcher

A small Linux daemon that turns a machine with a global IPv6 address on its WAN interface into an IPv6 gateway for a LAN. It derives a LAN prefix from the WAN address, configures the LAN interface, advertises the prefix to clients, hands out addresses with DHCPv6, and sets up NDP proxying so LAN hosts can be reached from the internet.

## What problem does it solve?

Many setups receive a single global IPv6 prefix from an ISP (often a /56 or /64) on one interface, but still need a working LAN side with:

- A stable router/gateway address on the LAN
- Prefix information for clients (Router Advertisements)
- Address assignment (DHCPv6)
- Reachability for LAN clients from outside the LAN (NDP proxy on the WAN)

Doing this by hand with `ip`, `sysctl`, `dnsmasq`, and neighbor tables is tedious and easy to get wrong, especially when the WAN prefix changes. **ipv6-watcher automates the full flow** and reacts when new clients appear on the LAN.

Typical use cases:

- A minimal Linux router or edge device (e.g. two NICs: WAN + LAN)
- Lab or homelab gateways where you want prefix splitting without a full router OS
- Environments where you need explicit NDP proxy entries for downstream IPv6 hosts

## What it does

On startup, ipv6-watcher:

1. **Reads** `config.json` (WAN/LAN interfaces, LAN prefix size, gateway IP position)
2. **Detects** the first global IPv6 address on the WAN interface
3. **Derives** a LAN prefix from that WAN address and picks a gateway IP (`first` or `last` host address in the subnet)
4. **Configures** the LAN interface: flushes old global addresses, assigns the gateway address and `fe80::1`, brings the link up
5. **Applies** kernel settings: enables NDP proxy on WAN and extends neighbor reachability time on LAN
6. **Starts** `dnsmasq` for stateful DHCPv6 on the LAN
7. **Sends** periodic IPv6 Router Advertisements (managed + other-config flags, on-link prefix)

Then it **watches the LAN** via netlink neighbor events. When a new IPv6 client appears inside the configured LAN prefix, it:

- Adds a host route for the client on the LAN
- Adds an NDP proxy entry on the WAN so the host is reachable from outside
- Sends an immediate Router Advertisement
- Resolves and caches the client MAC in the neighbor table

On exit, it stops the `dnsmasq` child process it started.

## Requirements

- Linux with IPv6 enabled
- Root privileges (netlink, raw packets, `sysctl`, `ip`, process management)
- [`dnsmasq`](https://thekelleys.org.uk/dnsmasq/doc.html) installed and available in `PATH`
- Go 1.26+ to build from source

## Configuration

Create `config.json` in the working directory:

```json
{
  "wan_interface": "eth0",
  "lan_interface": "eth1",
  "lan_subnet_size": 96,
  "lan_ip_position": "first",
  "lan_ula_address": "fd00:1111:cafe::1",
  "ula_dhcp_range": "fd00:1111:cafe::10,fd00:1111:cafe::ffff:fff6,64,12h"
}
```

| Field | Description |
|-------|-------------|
| `wan_interface` | Interface facing the upstream network (where the global IPv6 lives) |
| `lan_interface` | Interface serving LAN clients |
| `lan_subnet_size` | Prefix length for the derived LAN subnet (e.g. `96` for a /96) |
| `lan_ip_position` | Gateway IP within the LAN subnet: `"first"` (…::1) or `"last"` (all host bits set) |
| `lan_ula_address` | ULA address assigned on the LAN interface (default: `fd00:1111:cafe::1`) |
| `ula_dhcp_range` | dnsmasq DHCPv6 range for the ULA prefix (default: `fd00:1111:cafe::10,fd00:1111:cafe::ffff:fff6,64,12h`) |

The LAN prefix is computed from the leading bits of the WAN global address. DHCPv6 leases are drawn from the lower end of that subnet; the gateway uses the position you configured. A separate ULA DHCP range is also advertised via dnsmasq.

### Command-line overrides

```bash
sudo ./ipv6-watcher \
  -ula-address=fd00:1111:cafe::1 \
  -ula-dhcp-range=fd00:1111:cafe::10,fd00:1111:cafe::ffff:fff6,64,12h
```

| Flag | Description |
|------|-------------|
| `-config` | Path to configuration file (default: `config.json`) |
| `-ula-address` | Override `lan_ula_address` from config |
| `-ula-dhcp-range` | Override `ula_dhcp_range` from config |

## Build and run

```bash
go build -o ipv6-watcher .
sudo ./ipv6-watcher
```

The program expects `config.json` in the current directory and writes dnsmasq runtime files under `/var/run/`.

## How it fits together

```
                    ┌─────────────────┐
   ISP / upstream   │   WAN (eth0)    │
   global IPv6 ────►│  global /64     │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  ipv6-watcher   │
                    │  - split prefix │
                    │  - RA + DHCPv6  │
                    │  - NDP proxy    │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
   LAN clients      │   LAN (eth1)    │
   ◄── RA/DHCPv6 ──│  derived /96    │
                    └─────────────────┘
```

## License

MIT — see [LICENSE](LICENSE).
