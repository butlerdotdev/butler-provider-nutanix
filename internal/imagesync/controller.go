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

// Package imagesync provides ImageSync fulfillment for Nutanix providers
// during bootstrap. This controller watches ImageSync resources and creates
// images on the target Nutanix Prism Central cluster.
//
// NOTE: This code is duplicated from butler-controller/internal/controller/imagesync/
// because during bootstrap, only provider controllers are running (butler-controller
// isn't installed yet). In steady state, butler-controller handles ImageSync.
// Bug fixes must be applied to both locations until a shared module is extracted.
package imagesync

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	butlerv1alpha1 "github.com/butlerdotdev/butler-api/api/v1alpha1"
)

const (
	// Requeue intervals.
	requeueShort = 15 * time.Second
	requeueLong  = 60 * time.Second
	requeueReady = 10 * time.Minute

	nutanixAPIPath     = "/api/nutanix/v3"
	nutanixHTTPTimeout = 30 * time.Second
)

// Reconciler reconciles ImageSync resources for Nutanix providers.
type Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=butler.butlerlabs.dev,resources=imagesyncs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=butler.butlerlabs.dev,resources=imagesyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=butler.butlerlabs.dev,resources=imagesyncs/finalizers,verbs=update
// +kubebuilder:rbac:groups=butler.butlerlabs.dev,resources=providerconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=butler.butlerlabs.dev,resources=butlerconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	is := &butlerv1alpha1.ImageSync{}
	if err := r.Get(ctx, req.NamespacedName, is); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.V(1).Info("reconciling ImageSync", "name", is.Name, "phase", is.Status.Phase)

	// Get ProviderConfig to check if this is a Nutanix request
	pc, err := r.getProviderConfig(ctx, is)
	if err != nil {
		logger.V(1).Info("skipping ImageSync: cannot get ProviderConfig", "error", err)
		return ctrl.Result{}, nil
	}

	// Only handle Nutanix provider requests
	if pc.Spec.Provider != butlerv1alpha1.ProviderTypeNutanix {
		logger.V(1).Info("skipping non-Nutanix ImageSync", "provider", pc.Spec.Provider)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !is.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, is)
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(is, butlerv1alpha1.FinalizerImageSync) {
		controllerutil.AddFinalizer(is, butlerv1alpha1.FinalizerImageSync)
		if err := r.Update(ctx, is); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set initial phase if not set
	if is.Status.Phase == "" {
		is.SetPhase(butlerv1alpha1.ImageSyncPhasePending)
		is.Status.ObservedGeneration = is.Generation
		if err := r.Status().Update(ctx, is); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Fetch ButlerConfig for factory URL
	bc, err := r.getButlerConfig(ctx)
	if err != nil {
		return r.setFailed(ctx, is, "ButlerConfigNotFound", err.Error())
	}

	if !bc.IsImageFactoryConfigured() {
		return r.setFailed(ctx, is, "ImageFactoryNotConfigured", "ButlerConfig.spec.imageFactory is not configured")
	}

	// Dispatch based on phase
	switch is.Status.Phase {
	case butlerv1alpha1.ImageSyncPhasePending:
		return r.reconcilePending(ctx, is, pc, bc)
	case butlerv1alpha1.ImageSyncPhaseDownloading, butlerv1alpha1.ImageSyncPhaseUploading:
		return r.reconcileInProgress(ctx, is, pc)
	case butlerv1alpha1.ImageSyncPhaseFailed:
		return ctrl.Result{RequeueAfter: requeueLong}, nil
	case butlerv1alpha1.ImageSyncPhaseReady:
		return ctrl.Result{RequeueAfter: requeueReady}, nil
	}

	return ctrl.Result{}, nil
}

// reconcilePending initiates the Nutanix image sync.
func (r *Reconciler) reconcilePending(ctx context.Context, is *butlerv1alpha1.ImageSync, pc *butlerv1alpha1.ProviderConfig, bc *butlerv1alpha1.ButlerConfig) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	factoryURL := bc.GetImageFactoryURL()
	artifactURL := buildArtifactURL(factoryURL, is.Spec.FactoryRef, is.Spec.Format)
	imageName := buildProviderImageName(is)

	// Store artifact URL in status
	is.Status.ArtifactURL = artifactURL

	nxClient, err := r.newNutanixClient(ctx, pc)
	if err != nil {
		return r.setFailed(ctx, is, "CredentialsError", fmt.Sprintf("failed to create Nutanix client: %v", err))
	}

	// Check if image already exists
	existingUUID, err := nxClient.getImageByName(ctx, imageName)
	if err != nil {
		return r.setFailed(ctx, is, "NutanixAPIError", fmt.Sprintf("failed to check existing image: %v", err))
	}
	if existingUUID != "" {
		logger.Info("image already exists on Nutanix", "uuid", existingUUID, "name", imageName)
		return r.setReady(ctx, is, existingUUID)
	}

	if is.Spec.TransferMode == butlerv1alpha1.TransferModeProxy {
		return r.setFailed(ctx, is, "UnsupportedTransferMode",
			"proxy transfer mode for Nutanix is not yet implemented; use direct mode")
	}

	// Direct mode: create image with source_uri
	logger.Info("creating Nutanix image via direct download", "name", imageName, "url", artifactURL)

	taskUUID, err := nxClient.createImageDirect(ctx, imageName, artifactURL,
		fmt.Sprintf("Butler Image Factory: %s %s", is.Spec.FactoryRef.Version, is.Spec.FactoryRef.SchematicID[:8]))
	if err != nil {
		return r.setFailed(ctx, is, "CreateImageFailed", fmt.Sprintf("failed to create Nutanix image: %v", err))
	}

	// Store task UUID in status for polling
	is.SetPhase(butlerv1alpha1.ImageSyncPhaseDownloading)
	is.Status.ObservedGeneration = is.Generation
	is.Status.ProviderTaskID = taskUUID
	meta.SetStatusCondition(&is.Status.Conditions, metav1.Condition{
		Type:               butlerv1alpha1.ConditionTypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             butlerv1alpha1.ReasonImageDownloading,
		Message:            fmt.Sprintf("Nutanix Prism Central is downloading image (task: %s)", taskUUID),
		ObservedGeneration: is.Generation,
	})
	if err := r.Status().Update(ctx, is); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueShort}, nil
}

// reconcileInProgress polls the status of a Nutanix image task.
func (r *Reconciler) reconcileInProgress(ctx context.Context, is *butlerv1alpha1.ImageSync, pc *butlerv1alpha1.ProviderConfig) (ctrl.Result, error) {
	imageName := buildProviderImageName(is)

	nxClient, err := r.newNutanixClient(ctx, pc)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}

	taskUUID := is.Status.ProviderTaskID

	if taskUUID != "" {
		status, err := nxClient.getTaskStatus(ctx, taskUUID)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueShort}, nil
		}

		switch status.Status {
		case "SUCCEEDED":
			imageUUID := ""
			for _, ref := range status.EntityRefs {
				if ref.Kind == "image" {
					imageUUID = ref.UUID
					break
				}
			}
			if imageUUID == "" {
				imageUUID, _ = nxClient.getImageByName(ctx, imageName)
			}
			if imageUUID == "" {
				return r.setFailed(ctx, is, "ImageUUIDNotFound", "task succeeded but image UUID not found")
			}
			is.Status.ProviderTaskID = ""
			return r.setReady(ctx, is, imageUUID)

		case "FAILED":
			is.Status.ProviderTaskID = ""
			return r.setFailed(ctx, is, "NutanixTaskFailed",
				fmt.Sprintf("image creation task failed: %s", status.Message))
		}

		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}

	// No task UUID — try to find image by name
	imageUUID, err := nxClient.getImageByName(ctx, imageName)
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueShort}, nil
	}
	if imageUUID != "" {
		return r.setReady(ctx, is, imageUUID)
	}

	return r.setFailed(ctx, is, "ImageNotFound", "image not found on Nutanix and no active task")
}

// handleDeletion removes the finalizer.
func (r *Reconciler) handleDeletion(ctx context.Context, is *butlerv1alpha1.ImageSync) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(is, butlerv1alpha1.FinalizerImageSync) {
		return ctrl.Result{}, nil
	}

	logger.Info("removing ImageSync finalizer", "name", is.Name)

	if err := r.Get(ctx, types.NamespacedName{Name: is.Name, Namespace: is.Namespace}, is); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(is, butlerv1alpha1.FinalizerImageSync)
	return ctrl.Result{}, r.Update(ctx, is)
}

// setFailed sets the ImageSync to Failed phase.
func (r *Reconciler) setFailed(ctx context.Context, is *butlerv1alpha1.ImageSync, reason, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Error(fmt.Errorf("%s", message), "image sync failed", "reason", reason)

	is.SetFailure(reason, message)
	is.Status.ObservedGeneration = is.Generation
	meta.SetStatusCondition(&is.Status.Conditions, metav1.Condition{
		Type:               butlerv1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: is.Generation,
	})

	if err := r.Status().Update(ctx, is); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueLong}, nil
}

// setReady transitions the ImageSync to Ready phase.
func (r *Reconciler) setReady(ctx context.Context, is *butlerv1alpha1.ImageSync, providerImageRef string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("image sync ready", "providerImageRef", providerImageRef)

	is.SetPhase(butlerv1alpha1.ImageSyncPhaseReady)
	is.Status.ProviderImageRef = providerImageRef
	is.Status.ObservedGeneration = is.Generation
	is.Status.FailureReason = ""
	is.Status.FailureMessage = ""
	meta.SetStatusCondition(&is.Status.Conditions, metav1.Condition{
		Type:               butlerv1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             butlerv1alpha1.ReasonImageReady,
		Message:            fmt.Sprintf("Image synced to provider: %s", providerImageRef),
		ObservedGeneration: is.Generation,
	})

	if err := r.Status().Update(ctx, is); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueReady}, nil
}

// Helper methods

func (r *Reconciler) getProviderConfig(ctx context.Context, is *butlerv1alpha1.ImageSync) (*butlerv1alpha1.ProviderConfig, error) {
	pc := &butlerv1alpha1.ProviderConfig{}
	ns := is.Spec.ProviderConfigRef.Namespace
	if ns == "" {
		ns = is.Namespace
	}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      is.Spec.ProviderConfigRef.Name,
		Namespace: ns,
	}, pc); err != nil {
		return nil, fmt.Errorf("failed to get ProviderConfig %s/%s: %w", ns, is.Spec.ProviderConfigRef.Name, err)
	}
	return pc, nil
}

func (r *Reconciler) getButlerConfig(ctx context.Context) (*butlerv1alpha1.ButlerConfig, error) {
	bc := &butlerv1alpha1.ButlerConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: "butler"}, bc); err != nil {
		return nil, fmt.Errorf("failed to get ButlerConfig: %w", err)
	}
	return bc, nil
}

func (r *Reconciler) getProviderCredentials(ctx context.Context, pc *butlerv1alpha1.ProviderConfig) (map[string][]byte, error) {
	ns := pc.Spec.CredentialsRef.Namespace
	if ns == "" {
		ns = pc.Namespace
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: pc.Spec.CredentialsRef.Name, Namespace: ns}, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", ns, pc.Spec.CredentialsRef.Name, err)
	}
	return secret.Data, nil
}

// Nutanix HTTP client

type nutanixClient struct {
	httpClient *http.Client
	baseURL    string
	authHeader string
}

func (r *Reconciler) newNutanixClient(ctx context.Context, pc *butlerv1alpha1.ProviderConfig) (*nutanixClient, error) {
	creds, err := r.getProviderCredentials(ctx, pc)
	if err != nil {
		return nil, err
	}

	username := string(creds["username"])
	password := string(creds["password"])
	if username == "" || password == "" {
		return nil, fmt.Errorf("Nutanix credentials missing username or password")
	}

	if pc.Spec.Nutanix == nil {
		return nil, fmt.Errorf("ProviderConfig missing nutanix configuration")
	}

	endpoint := strings.TrimSuffix(pc.Spec.Nutanix.Endpoint, "/")
	port := pc.Spec.Nutanix.Port
	if port == 0 {
		port = 9440
	}
	baseURL := fmt.Sprintf("%s:%d%s", endpoint, port, nutanixAPIPath)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: pc.Spec.Nutanix.Insecure, //nolint:gosec // User-controlled config
		},
	}

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	return &nutanixClient{
		httpClient: &http.Client{Transport: transport, Timeout: nutanixHTTPTimeout},
		baseURL:    baseURL,
		authHeader: "Basic " + auth,
	}, nil
}

func (c *nutanixClient) createImageDirect(ctx context.Context, name, sourceURL, description string) (string, error) {
	body := map[string]interface{}{
		"api_version": "3.1",
		"metadata": map[string]interface{}{
			"kind": "image",
		},
		"spec": map[string]interface{}{
			"name":        name,
			"description": description,
			"resources": map[string]interface{}{
				"image_type": "DISK_IMAGE",
				"source_uri": sourceURL,
			},
		},
	}

	resp, err := c.doRequest(ctx, "POST", "/images", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create image failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var createResp struct {
		Status struct {
			ExecutionContext struct {
				TaskUUID string `json:"task_uuid"`
			} `json:"execution_context"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return createResp.Status.ExecutionContext.TaskUUID, nil
}

func (c *nutanixClient) getImageByName(ctx context.Context, name string) (string, error) {
	body := map[string]interface{}{
		"kind":   "image",
		"filter": fmt.Sprintf("name==%s", name),
	}

	resp, err := c.doRequest(ctx, "POST", "/images/list", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list images failed: status %d", resp.StatusCode)
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
		return "", err
	}

	for _, e := range listResp.Entities {
		if e.Status.Name == name {
			return e.Metadata.UUID, nil
		}
	}
	return "", nil
}

type nutanixTaskStatus struct {
	Status     string
	Message    string
	EntityRefs []struct {
		Kind string
		UUID string
	}
}

func (c *nutanixClient) getTaskStatus(ctx context.Context, taskUUID string) (*nutanixTaskStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/tasks/"+taskUUID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get task failed: status %d", resp.StatusCode)
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
		return nil, err
	}

	result := &nutanixTaskStatus{
		Status:  taskResp.Status,
		Message: taskResp.ProgressMessage,
	}
	for _, ref := range taskResp.EntityReferenceList {
		result.EntityRefs = append(result.EntityRefs, struct {
			Kind string
			UUID string
		}{Kind: ref.Kind, UUID: ref.UUID})
	}
	return result, nil
}

func (c *nutanixClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// Shared helper functions

// buildArtifactURL constructs the factory download URL for an image.
func buildArtifactURL(factoryURL string, ref butlerv1alpha1.ImageFactoryRef, format string) string {
	factoryURL = strings.TrimSuffix(factoryURL, "/")
	if format == "" {
		format = "qcow2"
	}
	arch := ref.Arch
	if arch == "" {
		arch = "amd64"
	}
	platform := ref.Platform
	if platform == "" {
		platform = "nocloud"
	}
	return fmt.Sprintf("%s/image/%s/%s/%s-%s.%s", factoryURL, ref.SchematicID, ref.Version, platform, arch, format)
}

// buildProviderImageName generates a deterministic image name for the provider.
func buildProviderImageName(is *butlerv1alpha1.ImageSync) string {
	if is.Spec.DisplayName != "" {
		return sanitizeName(is.Spec.DisplayName)
	}
	platform := is.Spec.FactoryRef.Platform
	if platform == "" {
		platform = "nocloud"
	}
	version := strings.ReplaceAll(is.Spec.FactoryRef.Version, ".", "-")
	arch := is.Spec.FactoryRef.Arch
	if arch == "" {
		arch = "amd64"
	}
	schematicPrefix := is.Spec.FactoryRef.SchematicID
	if len(schematicPrefix) > 8 {
		schematicPrefix = schematicPrefix[:8]
	}
	name := fmt.Sprintf("%s-%s-%s-%s-butler", platform, version, arch, schematicPrefix)
	return sanitizeName(name)
}

var invalidDNSChars = regexp.MustCompile(`[^a-z0-9.-]`)

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = invalidDNSChars.ReplaceAllString(name, "")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if len(name) > 63 {
		name = name[:63]
	}
	name = strings.Trim(name, "-.")
	return name
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&butlerv1alpha1.ImageSync{}).
		Named("imagesync").
		Complete(r)
}
