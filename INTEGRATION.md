# Butler Provider Nutanix - Integration Guide

## Scaffolding Complete ✅

The `butler-provider-nutanix` project has been scaffolded following the exact same patterns as `butler-provider-harvester`.

## Project Structure Comparison

| Component | Harvester | Nutanix |
|-----------|-----------|---------|
| Controller | `internal/controller/machinerequest_controller.go` | ✅ Same structure |
| Client | `internal/harvester/client.go` | `internal/nutanix/client.go` |
| Types | `internal/harvester/types.go` | `internal/nutanix/types.go` |
| API | Kubernetes API (KubeVirt) | REST API (Prism Central v3) |
| Auth | Kubeconfig | Username/Password |

## Key Differences

### Authentication
- **Harvester**: Uses kubeconfig from secret
- **Nutanix**: Uses username/password from secret with keys `username` and `password`

### VM Creation
- **Harvester**: Creates PVC + VirtualMachine CRD via Kubernetes API
- **Nutanix**: POST to `/api/nutanix/v3/vms` with full VM spec

### VM Status
- **Harvester**: Gets VMI and checks interfaces for IP
- **Nutanix**: GET `/api/nutanix/v3/vms/{uuid}` and check nic_list

## Integration with butler-bootstrap

### 1. Update butler-cli orchestrator

Add Nutanix provider to `buildProviderConfigUnstructured()`:

```go
case "nutanix":
    spec["nutanix"] = map[string]interface{}{
        "endpoint":    cfg.Nutanix.Endpoint,
        "port":        cfg.Nutanix.Port,
        "insecure":    cfg.Nutanix.Insecure,
        "clusterUUID": cfg.Nutanix.ClusterUUID,
        "subnetUUID":  cfg.Nutanix.SubnetUUID,
        "imageUUID":   cfg.Nutanix.ImageUUID,
    }
```

### 2. Add bootstrap config support

Add Nutanix config to `Config` struct:

```go
type NutanixConfig struct {
    Endpoint     string `yaml:"endpoint"`
    Port         int32  `yaml:"port"`
    Insecure     bool   `yaml:"insecure"`
    Username     string `yaml:"username"`
    Password     string `yaml:"password"`
    ClusterUUID  string `yaml:"clusterUUID"`
    SubnetUUID   string `yaml:"subnetUUID"`
    ImageUUID    string `yaml:"imageUUID"`
}

type Config struct {
    // ...
    Nutanix *NutanixConfig `yaml:"nutanix,omitempty"`
}
```

### 3. Embed controller manifest

Create `butler-provider-nutanix.yaml` for embedding in butler-cli:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: butler-provider-nutanix
  namespace: butler-system
spec:
  replicas: 1
  selector:
    matchLabels:
      control-plane: butler-provider-nutanix
  template:
    spec:
      containers:
      - name: manager
        image: ghcr.io/butlerdotdev/butler-provider-nutanix:latest
        # ... (same structure as harvester)
```

### 4. Add `butleradm bootstrap nutanix` command

Mirror the harvester command structure.

## Example Bootstrap Config (Nutanix)

```yaml
provider: nutanix
cluster:
  name: butler-alpha
  vip: 10.50.0.100
  metallb:
    start: 10.50.0.110
    end: 10.50.0.150
  talos:
    version: v1.9.0
nutanix:
  endpoint: https://prism-central.example.com
  port: 9440
  insecure: false
  username: admin
  password: ${NUTANIX_PASSWORD}
  clusterUUID: "00000000-0000-0000-0000-000000000000"
  subnetUUID: "11111111-1111-1111-1111-111111111111"
  imageUUID: "22222222-2222-2222-2222-222222222222"  # Talos image
controlPlanes:
  count: 3
  cpu: 4
  memoryMB: 8192
  diskGB: 50
workers:
  count: 3
  cpu: 8
  memoryMB: 16384
  diskGB: 100
```

## Testing Plan

1. **Unit Tests**: Mock Nutanix API responses
2. **Integration Tests**: Use Nutanix test environment
3. **E2E Tests**: Full bootstrap on Nutanix cluster

## Next Steps

1. [ ] Create GitHub repo `butlerdotdev/butler-provider-nutanix`
2. [ ] Push scaffolded code
3. [ ] Set up CI/CD (uses self-hosted runners)
4. [ ] Update butler-cli with Nutanix support
5. [ ] Test against real Nutanix environment
6. [ ] Refine VM spec (cloud-init handling, guest tools)

## Files to Copy

All files are in `/home/claude/butler-provider-nutanix/`

```bash
# Copy to your local machine
cp -r /home/claude/butler-provider-nutanix ~/code/github.com/butlerdotdev/
cd ~/code/github.com/butlerdotdev/butler-provider-nutanix
git init
git add .
git commit -m "Initial scaffold of butler-provider-nutanix"
```
