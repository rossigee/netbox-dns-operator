# NetBox DNS Operator

A Kubernetes operator that synchronizes DNS zone files from NetBox as the single source of truth for network infrastructure.

## Overview

The NetBox DNS Operator automatically generates and maintains DNS zone files based on data from NetBox. It creates ConfigMaps containing BIND-formatted zone files that can be consumed by CoreDNS or other DNS servers.

## Features

- **NetBox Integration**: Fetches device and IP address data from NetBox API
- **Automatic Zone Generation**: Creates DNS zone files with A, PTR, and SOA records
- **ConfigMap Management**: Stores zone files in Kubernetes ConfigMaps
- **Webhook Support**: Can be triggered by NetBox webhooks for real-time updates
- **Periodic Reconciliation**: Falls back to scheduled checks for reliability

## Architecture

```
NetBox API ──► Operator ──► ConfigMaps ──► CoreDNS
     ▲              │              │
     │              ▼              ▼
Webhooks ─────► Controller ──► Zone Files ──► DNS Resolution
```

## Usage

### 1. Install the Operator

```bash
make deploy
```

### 2. Create a NetBoxDNSOperator Resource

```yaml
apiVersion: netbox-dns-operator.rossigee.github.com/v1
kind: NetBoxDNSOperator
metadata:
  name: netbox-dns-sync
  namespace: dns-system
spec:
  netboxURL: "https://netbox.example.com"
  netboxToken: "your-netbox-api-token"
  zones:
    - "foo.lan"
    - "bar.lan"
  reloadInterval: "5m"
  webhookURL: "https://operator.example.com/webhook"
```

### 3. Generated ConfigMaps

The operator will create ConfigMaps like:
- `coredns-foo-lan-zone` containing the `foo.lan` zone file
- `coredns-bar-lan-zone` containing the `bar.lan` zone file

### 4. CoreDNS Configuration

Update your CoreDNS deployment to use the generated zone files:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns-config
data:
    Corefile: |
    .:53 {
        # Load zones from operator-generated ConfigMaps
        file /etc/coredns/zones/foo.lan
        file /etc/coredns/zones/bar.lan

        # Forward other queries
        forward . 8.8.8.8 1.1.1.1
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: coredns
spec:
  template:
    spec:
      volumes:
      - name: zones
        projected:
          sources:
          - configMap:
              name: coredns-foo-lan-zone
          - configMap:
              name: coredns-bar-lan-zone
      containers:
      - name: coredns
        volumeMounts:
        - name: zones
          mountPath: /etc/coredns/zones
```

## Development

### Prerequisites

- Go 1.21+
- Kubernetes cluster
- NetBox instance with API access

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Running Locally

```bash
make run
```

## Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `netboxURL` | NetBox API endpoint | Required |
| `netboxToken` | NetBox API token | Required |
| `zones` | List of DNS zones to manage | Required |
| `reloadInterval` | How often to sync (Go duration) | `5m` |
| `webhookURL` | URL for NetBox webhook notifications | Optional |

## NetBox Data Sources

The operator fetches the following data from NetBox:

- **Devices**: Used for A records (`device.name -> device.primary_ip`)
- **IP Addresses**: Used for PTR records (`ip.address -> ip.dns_name`)
- **VMs**: Treated as devices for DNS purposes
- **Services**: Can be extended for SRV records

## DNS Record Types

Currently supports:
- **SOA**: Zone authority records with auto-incrementing serials
- **NS**: Name server records
- **A**: IPv4 address records
- **PTR**: Reverse DNS records

Future enhancements may include:
- **AAAA**: IPv6 records
- **CNAME**: Canonical name records
- **MX**: Mail exchange records
- **TXT**: Text records
- **SRV**: Service location records

## Security

- NetBox API tokens should be stored in Kubernetes secrets
- Operator runs with minimal RBAC permissions
- Network policies should restrict operator access

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

Apache License 2.0
