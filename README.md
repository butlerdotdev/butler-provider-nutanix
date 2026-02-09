# Butler Provider Nutanix

Nutanix AHV infrastructure provider for Butler. This controller handles `MachineRequest` CRs for Nutanix Prism Central.

## Overview

The butler-provider-nutanix controller watches for `MachineRequest` resources that reference a `ProviderConfig` with `provider: nutanix`. It uses the Nutanix Prism Central v3 API to create, monitor, and delete VMs.

## Prerequisites

### Prism Central Requirements

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| Prism Central Version | pc.2022.6 | pc.2024.1+ |
| AOS Version | 6.5 | 6.8+ |
| API Version | v3 | v3 |

### Required Permissions

The Prism Central user must have these permissions:

| Permission | Scope | Purpose |
|------------|-------|---------|
| `VM:Create` | Cluster | Create VMs for management/tenant clusters |
| `VM:Delete` | Cluster | Delete VMs during scale-down or cleanup |
| `VM:Update` | Cluster | Modify VM configuration |
| `VM:View` | Cluster | Monitor VM status and retrieve IPs |
| `Image:View` | Cluster | Access OS images |
| `Subnet:View` | Cluster | Access network subnets |
| `Cluster:View` | Cluster | Access cluster resources |

**Recommended**: Create a dedicated service account with a custom role containing only these permissions.

### Required Resources

1. **Cluster UUID**: The AHV cluster where VMs will be created
2. **Subnet UUID**: Network subnet for VM NICs
3. **Image UUID**: Talos Linux disk image (uploaded to Prism Central)

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

## Finding UUIDs

Butler requires UUIDs for Nutanix resources. Here's how to find them:

### Via Prism Central UI

1. **Cluster UUID**:
   - Navigate to Hardware → Clusters
   - Click on your cluster
   - The UUID is in the URL: `.../#/clusters/{cluster-uuid}/...`

2. **Subnet UUID**:
   - Navigate to Network → Subnets
   - Click on your subnet
   - The UUID is in the URL: `.../#/subnets/{subnet-uuid}/...`

3. **Image UUID**:
   - Navigate to Compute → Images
   - Click on your Talos image
   - The UUID is in the URL or visible in the details panel

### Via Prism Central API

```bash
# Set your Prism Central endpoint
PC_ENDPOINT="https://prism-central.example.com:9440"
PC_USER="admin"
PC_PASS="your-password"

# List clusters
curl -sk -u "${PC_USER}:${PC_PASS}" \
  "${PC_ENDPOINT}/api/nutanix/v3/clusters/list" \
  -H "Content-Type: application/json" \
  -d '{"kind":"cluster"}' | jq '.entities[] | {name: .spec.name, uuid: .metadata.uuid}'

# List subnets
curl -sk -u "${PC_USER}:${PC_PASS}" \
  "${PC_ENDPOINT}/api/nutanix/v3/subnets/list" \
  -H "Content-Type: application/json" \
  -d '{"kind":"subnet"}' | jq '.entities[] | {name: .spec.name, uuid: .metadata.uuid, vlan: .spec.resources.vlan_id}'

# List images
curl -sk -u "${PC_USER}:${PC_PASS}" \
  "${PC_ENDPOINT}/api/nutanix/v3/images/list" \
  -H "Content-Type: application/json" \
  -d '{"kind":"image"}' | jq '.entities[] | {name: .spec.name, uuid: .metadata.uuid}'
```

### Via ntnx CLI (if installed)

```bash
# List clusters
ntnx cluster list

# List subnets
ntnx subnet list

# List images
ntnx image list
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

## Troubleshooting

### VM Creation Fails with 401 Unauthorized

**Symptoms**: Controller logs show authentication failures.

**Solutions**:
1. Verify credentials in the Secret are correct
2. Check if the user account is locked or disabled
3. Ensure the password doesn't contain special characters that need escaping
4. Verify Prism Central endpoint URL is correct (include port 9440)

### VM Creation Fails with 403 Forbidden

**Symptoms**: Authentication succeeds but operations fail with permission errors.

**Solutions**:
1. Verify the user has the required permissions (see Prerequisites)
2. Check if the user has access to the specified cluster
3. For project-scoped users, ensure the cluster/subnet/image are in an accessible project

### VM Stuck in Creating State

**Symptoms**: VM appears in Prism Central but MachineRequest never transitions to Running.

**Possible causes**:
1. **No IP assigned**: Check if DHCP is available on the subnet
2. **Task not completing**: Check Prism Central Tasks for errors
3. **Image boot failure**: Verify the image is a valid Talos Linux image

**Debug steps**:
```bash
# Check VM status in Prism Central
curl -sk -u "${PC_USER}:${PC_PASS}" \
  "${PC_ENDPOINT}/api/nutanix/v3/vms/{vm-uuid}" | jq '.status.resources.nic_list[].ip_endpoint_list'
```

### Network Connectivity Issues

**Symptoms**: VMs created but cannot reach each other.

**Checks**:
1. Verify all VMs are on the same subnet
2. Check AHV microsegmentation policies
3. Ensure VLAN is properly trunked to all AHV hosts
4. Verify no flow rules blocking traffic

### Certificate Errors

**Symptoms**: TLS handshake failures connecting to Prism Central.

**Solutions**:
1. Set `insecure: true` in ProviderConfig (not recommended for production)
2. Add Prism Central's CA certificate to the trusted store
3. Verify the endpoint hostname matches the certificate CN/SAN

## Version Compatibility

| Butler Version | Prism Central | AOS | Status |
|----------------|---------------|-----|--------|
| v0.1.x | pc.2024.1+ | 6.8+ | Supported |
| v0.1.x | pc.2023.x | 6.5+ | Supported |
| v0.1.x | pc.2022.x | 6.1+ | Deprecated |

## License

Apache License 2.0
