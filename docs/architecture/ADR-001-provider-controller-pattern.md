# ADR-001: Provider Controller Pattern

## Status

Accepted

## Context

Butler needs to support multiple infrastructure providers (Harvester, Nutanix, Proxmox, cloud providers). Each provider has different APIs and authentication mechanisms.

## Decision

We implement each provider as a separate controller that:

1. **Watches MachineRequest CRs** - All providers watch the same CRD type
2. **Filters by ProviderConfig** - Each controller only processes requests matching its provider type
3. **Implements provider-specific client** - Encapsulates provider API details
4. **Uses identical reconciliation phases** - Pending → Creating → Running

### Key Design Choices

1. **Separate repositories per provider** - Enables independent versioning and deployment
2. **Shared CRD types in butler-api** - Single source of truth for API
3. **Provider check at reconcile start** - Early exit for non-matching providers
4. **Provider-specific finalizers** - Prevent deletion until provider cleanup is complete

## Implementation

```
MachineRequest
     │
     ├──► butler-provider-harvester (if provider=harvester)
     │         └── Uses Kubernetes API (KubeVirt)
     │
     ├──► butler-provider-nutanix (if provider=nutanix)
     │         └── Uses Prism Central REST API
     │
     └──► butler-provider-proxmox (if provider=proxmox)
               └── Uses Proxmox REST API
```

### Provider Client Interface (Conceptual)

Each provider implements these operations:
- `CreateVM(opts VMCreateOptions) (providerID string, error)`
- `GetVMStatus(providerID string) (*VMStatus, error)`
- `DeleteVM(providerID string) error`

### ProviderConfig Selection

```go
// Only handle requests for our provider type
if providerConfig.Spec.Provider != butlerv1alpha1.ProviderTypeNutanix {
    return ctrl.Result{}, nil
}
```

## Consequences

### Positive
- Clean separation of provider logic
- Independent testing and deployment
- Easy to add new providers
- No cross-provider dependencies

### Negative
- Some code duplication in controller structure
- Multiple deployments to manage
- Need to keep provider versions in sync with butler-api

## Notes

This pattern is similar to how Cluster API implements infrastructure providers, aligning with CNCF best practices.
