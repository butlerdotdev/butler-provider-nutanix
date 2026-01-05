# Butler Provider Nutanix

Nutanix AHV infrastructure provider for Butler. This controller handles `MachineRequest` CRs for Nutanix Prism Central.

## Overview

The butler-provider-nutanix controller watches for `MachineRequest` resources that reference a `ProviderConfig` with `provider: nutanix`. It uses the Nutanix Prism Central v3 API to create, monitor, and delete VMs.

## Prerequisites

- Nutanix Prism Central (PC) with API access
- PC user credentials with VM management permissions
- Network subnet UUID for VM placement
- (Optional) Image UUID for OS template

## ProviderConfig Example

```yaml
apiVersion: butler.butlerlabs.dev/v1alpha1
kind: ProviderConfig
metadata:
  name: nutanix-provider
  namespace: butler-system
spec:
  provider: nutanix
  credentialsRef:
    name: nutanix-credentials
    namespace: butler-system
  nutanix:
    endpoint: "https://prism-central.example.com"
    port: 9440
    insecure: false
    clusterUUID: "00000000-0000-0000-0000-000000000000"
    subnetUUID: "11111111-1111-1111-1111-111111111111"
    imageUUID: "22222222-2222-2222-2222-222222222222"
```

## Credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nutanix-credentials
  namespace: butler-system
type: Opaque
stringData:
  username: admin
  password: your-password-here
```

## Building

```bash
# Build binary
make build

# Build container image
make docker-build IMG=ghcr.io/butlerdotdev/butler-provider-nutanix:latest

# Push image
make docker-push IMG=ghcr.io/butlerdotdev/butler-provider-nutanix:latest
```

## Deployment

The provider is typically deployed as part of the Butler bootstrap process. For manual deployment:

```bash
make deploy IMG=ghcr.io/butlerdotdev/butler-provider-nutanix:latest
```

## Architecture

```
MachineRequest (nutanix provider)
        │
        ▼
┌─────────────────────────┐
│ butler-provider-nutanix │
│    MachineRequest       │
│    Reconciler           │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│ Nutanix Prism Central   │
│ v3 REST API             │
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│ Nutanix AHV Cluster     │
│ (VMs created here)      │
└─────────────────────────┘
```

## VM Lifecycle

1. **Pending** → Create VM via Prism Central API
2. **Creating** → Poll for IP address
3. **Running** → Monitor for drift
4. **Deleting** → Delete VM via API

## License

Apache License 2.0
