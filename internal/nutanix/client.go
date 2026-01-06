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

package nutanix

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	butlerv1alpha1 "github.com/butlerdotdev/butler-api/api/v1alpha1"
)

const (
	// Prism Central API v3 base path
	apiV3Path = "/api/nutanix/v3"

	// Default HTTP timeout
	defaultTimeout = 30 * time.Second
)

// Client provides access to Nutanix Prism Central resources.
type Client struct {
	httpClient *http.Client
	baseURL    string
	authHeader string
	config     *butlerv1alpha1.NutanixProviderConfig
}

// NewClient creates a new Nutanix Prism Central client.
func NewClient(username, password string, config *butlerv1alpha1.NutanixProviderConfig) (*Client, error) {
	// Build base URL
	endpoint := strings.TrimSuffix(config.Endpoint, "/")
	port := config.Port
	if port == 0 {
		port = 9440
	}
	baseURL := fmt.Sprintf("%s:%d%s", endpoint, port, apiV3Path)

	// Create HTTP client with optional TLS skip
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: config.Insecure, //nolint:gosec // User-controlled config
		},
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}

	// Build basic auth header
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	authHeader := "Basic " + auth

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		authHeader: authHeader,
		config:     config,
	}, nil
}

// VMCreateOptions defines options for creating a VM.
type VMCreateOptions struct {
	Name        string
	CPU         int32
	MemoryMB    int32
	DiskGB      int32
	ImageUUID   string            // Nutanix image UUID
	UserData    string            // cloud-init userdata (base64)
	NetworkData string            // cloud-init networkdata (base64)
	Labels      map[string]string // VM categories/labels
}

// VMStatus represents the status of a VM.
type VMStatus struct {
	UUID       string
	Name       string
	PowerState string
	IPAddress  string
	MACAddress string
}

// VMInfo contains basic VM info for lookups.
type VMInfo struct {
	UUID string
	Name string
}

// CreateVM creates a new VM in Nutanix.
func (c *Client) CreateVM(ctx context.Context, opts VMCreateOptions) (string, error) {
	// Use image from options or fall back to config default
	imageUUID := opts.ImageUUID
	if imageUUID == "" {
		imageUUID = c.config.ImageUUID
	}
	if imageUUID == "" {
		return "", fmt.Errorf("no image UUID specified and no default image in provider config")
	}

	// Build the VM spec for Prism Central v3 API
	vmSpec := c.buildVMSpec(opts, imageUUID)

	// Create the VM
	resp, err := c.doRequest(ctx, "POST", "/vms", vmSpec)
	if err != nil {
		return "", fmt.Errorf("failed to create VM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create VM: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response to get task UUID, then wait for VM UUID
	var createResp struct {
		Status struct {
			ExecutionContext struct {
				TaskUUID string `json:"task_uuid"`
			} `json:"execution_context"`
		} `json:"status"`
		Metadata struct {
			UUID string `json:"uuid"`
		} `json:"metadata"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", fmt.Errorf("failed to decode create response: %w", err)
	}

	// Always wait for the task to complete - metadata.uuid is returned immediately
	// but the VM isn't ready until the task finishes
	if createResp.Status.ExecutionContext.TaskUUID != "" {
		return c.waitForTask(ctx, createResp.Status.ExecutionContext.TaskUUID)
	}

	// Fall back to metadata UUID only if no task (shouldn't happen for create)
	if createResp.Metadata.UUID != "" {
		return createResp.Metadata.UUID, nil
	}

	return "", fmt.Errorf("no UUID or task UUID in create response")
}

// buildVMSpec constructs the Prism Central v3 VM spec.
func (c *Client) buildVMSpec(opts VMCreateOptions, imageUUID string) map[string]interface{} {
	// Calculate disk size in bytes
	diskSizeBytes := int64(opts.DiskGB) * 1024 * 1024 * 1024

	// Build NIC list
	nicList := []map[string]interface{}{
		{
			"subnet_reference": map[string]interface{}{
				"kind": "subnet",
				"uuid": c.config.SubnetUUID,
			},
		},
	}

	// Build disk list - just the boot disk from image
	diskList := []map[string]interface{}{
		{
			"device_properties": map[string]interface{}{
				"device_type": "DISK",
				"disk_address": map[string]interface{}{
					"adapter_type": "SCSI",
					"device_index": 0,
				},
			},
			"data_source_reference": map[string]interface{}{
				"kind": "image",
				"uuid": imageUUID,
			},
			"disk_size_bytes": diskSizeBytes,
		},
	}

	// Build resources spec
	resources := map[string]interface{}{
		"num_sockets":          1,
		"num_vcpus_per_socket": opts.CPU,
		"memory_size_mib":      opts.MemoryMB,
		"power_state":          "ON",
		"nic_list":             nicList,
		"disk_list":            diskList,
	}

	if opts.UserData != "" {
		cloudInitConfig := map[string]interface{}{
			"user_data": opts.UserData,
		}
		if opts.NetworkData != "" {
			cloudInitConfig["meta_data"] = opts.NetworkData
		}
		resources["guest_customization"] = map[string]interface{}{
			"cloud_init": cloudInitConfig,
		}
	}

	spec := map[string]interface{}{
		"api_version": "3.1",
		"metadata": map[string]interface{}{
			"kind": "vm",
		},
		"spec": map[string]interface{}{
			"name":      opts.Name,
			"resources": resources,
			"cluster_reference": map[string]interface{}{
				"kind": "cluster",
				"uuid": c.config.ClusterUUID,
			},
		},
	}

	// Add categories/labels if provided
	if len(opts.Labels) > 0 {
		categories := make(map[string]string)
		for k, v := range opts.Labels {
			categories[k] = v
		}
		spec["metadata"].(map[string]interface{})["categories"] = categories
	}

	return spec
}

// GetVMStatus returns the current status of a VM by UUID.
func (c *Client) GetVMStatus(ctx context.Context, uuid string) (*VMStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/vms/"+uuid, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotFoundError{Resource: "vm", UUID: uuid}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get VM: status %d, body: %s", resp.StatusCode, string(body))
	}

	var vmResp struct {
		Metadata struct {
			UUID string `json:"uuid"`
		} `json:"metadata"`
		Status struct {
			Name      string `json:"name"`
			Resources struct {
				PowerState string `json:"power_state"`
				NicList    []struct {
					IPEndpointList []struct {
						IP string `json:"ip"`
					} `json:"ip_endpoint_list"`
					MacAddress string `json:"mac_address"`
				} `json:"nic_list"`
			} `json:"resources"`
		} `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&vmResp); err != nil {
		return nil, fmt.Errorf("failed to decode VM response: %w", err)
	}

	status := &VMStatus{
		UUID:       vmResp.Metadata.UUID,
		Name:       vmResp.Status.Name,
		PowerState: vmResp.Status.Resources.PowerState,
	}

	// Extract IP and MAC from first NIC with an IP
	for _, nic := range vmResp.Status.Resources.NicList {
		if len(nic.IPEndpointList) > 0 {
			for _, ep := range nic.IPEndpointList {
				if ep.IP != "" && isUsableIP(ep.IP) {
					status.IPAddress = ep.IP
					status.MACAddress = nic.MacAddress
					break
				}
			}
			if status.IPAddress != "" {
				break
			}
		}
	}

	return status, nil
}

// GetVMByName finds a VM by name (returns first match).
func (c *Client) GetVMByName(ctx context.Context, name string) (*VMInfo, error) {
	// Use list with filter
	filter := map[string]interface{}{
		"kind":   "vm",
		"filter": fmt.Sprintf("vm_name==%s", name),
	}

	resp, err := c.doRequest(ctx, "POST", "/vms/list", filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list VMs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list VMs: status %d, body: %s", resp.StatusCode, string(body))
	}

	var listResp struct {
		Entities []struct {
			Metadata struct {
				UUID string `json:"uuid"`
			} `json:"metadata"`
			Status struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"entities"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode list response: %w", err)
	}

	for _, entity := range listResp.Entities {
		if entity.Status.Name == name {
			return &VMInfo{
				UUID: entity.Metadata.UUID,
				Name: entity.Status.Name,
			}, nil
		}
	}

	return nil, nil
}

// DeleteVM deletes a VM by UUID.
func (c *Client) DeleteVM(ctx context.Context, uuid string) error {
	resp, err := c.doRequest(ctx, "DELETE", "/vms/"+uuid, nil)
	if err != nil {
		return fmt.Errorf("failed to delete VM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Resource: "vm", UUID: uuid}
	}

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete VM: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// waitForTask waits for a Nutanix task to complete and returns the entity UUID.
func (c *Client) waitForTask(ctx context.Context, taskUUID string) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", fmt.Errorf("timeout waiting for task %s", taskUUID)
		case <-ticker.C:
			resp, err := c.doRequest(ctx, "GET", "/tasks/"+taskUUID, nil)
			if err != nil {
				continue
			}

			var taskResp struct {
				Status              string `json:"status"`
				ProgressMessage     string `json:"progress_message"`
				EntityReferenceList []struct {
					Kind string `json:"kind"`
					UUID string `json:"uuid"`
				} `json:"entity_reference_list"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			switch taskResp.Status {
			case "SUCCEEDED":
				// Find the VM UUID in entity references
				for _, ref := range taskResp.EntityReferenceList {
					if ref.Kind == "vm" {
						return ref.UUID, nil
					}
				}
				return "", fmt.Errorf("task succeeded but no VM UUID in response")
			case "FAILED":
				return "", fmt.Errorf("task failed: %s", taskResp.ProgressMessage)
			}
			// Still running, continue polling
		}
	}
}

// doRequest performs an HTTP request to Prism Central.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// isUsableIP returns true if the IP is a routable IPv4 address.
func isUsableIP(ip string) bool {
	// Skip IPv6 (contains colons)
	if strings.Contains(ip, ":") {
		return false
	}
	// Skip IPv4 link-local (169.254.x.x)
	if strings.HasPrefix(ip, "169.254.") {
		return false
	}
	return true
}

// NotFoundError indicates a resource was not found.
type NotFoundError struct {
	Resource string
	UUID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %s not found", e.Resource, e.UUID)
}

// IsNotFound returns true if the error is a NotFoundError.
func IsNotFound(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}
