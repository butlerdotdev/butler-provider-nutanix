/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package nutanix provides a client for Nutanix Prism Central API.
package nutanix

// VMPowerState represents the power state of a Nutanix VM.
type VMPowerState string

const (
	// PowerStateOn indicates the VM is powered on.
	PowerStateOn VMPowerState = "ON"

	// PowerStateOff indicates the VM is powered off.
	PowerStateOff VMPowerState = "OFF"

	// PowerStatePaused indicates the VM is paused.
	PowerStatePaused VMPowerState = "PAUSED"

	// PowerStateSuspended indicates the VM is suspended.
	PowerStateSuspended VMPowerState = "SUSPENDED"
)

// TaskStatus represents the status of a Nutanix task.
type TaskStatus string

const (
	// TaskStatusQueued indicates the task is queued.
	TaskStatusQueued TaskStatus = "QUEUED"

	// TaskStatusRunning indicates the task is running.
	TaskStatusRunning TaskStatus = "RUNNING"

	// TaskStatusSucceeded indicates the task succeeded.
	TaskStatusSucceeded TaskStatus = "SUCCEEDED"

	// TaskStatusFailed indicates the task failed.
	TaskStatusFailed TaskStatus = "FAILED"

	// TaskStatusAborted indicates the task was aborted.
	TaskStatusAborted TaskStatus = "ABORTED"
)

// EntityReference represents a reference to a Nutanix entity.
type EntityReference struct {
	Kind string `json:"kind"`
	UUID string `json:"uuid"`
	Name string `json:"name,omitempty"`
}

// VMSpec represents the specification for creating a Nutanix VM.
// This is a subset of the full Prism Central v3 VM spec.
type VMSpec struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Resources   VMResources       `json:"resources"`
	Categories  map[string]string `json:"categories,omitempty"`
}

// VMResources represents the resources allocated to a VM.
type VMResources struct {
	NumSockets          int32          `json:"num_sockets"`
	NumVCPUsPerSocket   int32          `json:"num_vcpus_per_socket"`
	MemorySizeMib       int32          `json:"memory_size_mib"`
	PowerState          VMPowerState   `json:"power_state"`
	NicList             []NicSpec      `json:"nic_list,omitempty"`
	DiskList            []DiskSpec     `json:"disk_list,omitempty"`
	GuestCustomization  *GuestCustomization `json:"guest_customization,omitempty"`
}

// NicSpec represents a NIC specification.
type NicSpec struct {
	SubnetReference EntityReference `json:"subnet_reference"`
	IPEndpointList  []IPEndpoint    `json:"ip_endpoint_list,omitempty"`
}

// IPEndpoint represents an IP endpoint on a NIC.
type IPEndpoint struct {
	IP   string `json:"ip,omitempty"`
	Type string `json:"type,omitempty"` // ASSIGNED, LEARNED
}

// DiskSpec represents a disk specification.
type DiskSpec struct {
	DeviceProperties    *DeviceProperties `json:"device_properties,omitempty"`
	DiskSizeBytes       int64             `json:"disk_size_bytes,omitempty"`
	DataSourceReference *EntityReference  `json:"data_source_reference,omitempty"`
}

// DeviceProperties represents disk device properties.
type DeviceProperties struct {
	DeviceType  string       `json:"device_type"` // DISK, CDROM
	DiskAddress *DiskAddress `json:"disk_address,omitempty"`
}

// DiskAddress represents a disk address.
type DiskAddress struct {
	AdapterType string `json:"adapter_type"` // SCSI, IDE, PCI, SATA, SPAPR
	DeviceIndex int    `json:"device_index"`
}

// GuestCustomization represents guest customization (cloud-init).
type GuestCustomization struct {
	CloudInit *CloudInitConfig `json:"cloud_init,omitempty"`
	Sysprep   *SysprepConfig   `json:"sysprep,omitempty"`
}

// CloudInitConfig represents cloud-init configuration.
type CloudInitConfig struct {
	UserData    string `json:"user_data,omitempty"`    // base64 encoded
	MetaData    string `json:"meta_data,omitempty"`    // base64 encoded
	CustomKeyValues map[string]string `json:"custom_key_values,omitempty"`
}

// SysprepConfig represents Sysprep configuration (Windows).
type SysprepConfig struct {
	InstallType   string `json:"install_type,omitempty"` // PREPARED, FRESH
	UnattendXML   string `json:"unattend_xml,omitempty"` // base64 encoded
	CustomKeyValues map[string]string `json:"custom_key_values,omitempty"`
}
