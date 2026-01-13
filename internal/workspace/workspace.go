package workspace

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Label for SGS workspace ID
	LabelWorkspaceID = "sgs.snucse.org/id"
	// Annotation for node selector
	AnnotationNodeSelector = "scheduler.alpha.kubernetes.io/node-selector"
)

// WorkspaceInfo represents information about an SGS workspace
type WorkspaceInfo struct {
	Name      string
	NodeGroup string // from node selector annotation
	GPUQuota  int64
	CPUQuota  string
	MemQuota  string
}

// List returns all workspaces the user has access to
func List(ctx context.Context, c *client.Client) ([]WorkspaceInfo, error) {
	// List all namespaces with SGS workspace label (with retry)
	var namespaces *corev1.NamespaceList
	var err error
	for i := 0; i < 2; i++ {
		namespaces, err = c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
			LabelSelector: LabelWorkspaceID,
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Check access to each namespace in parallel
	type result struct {
		info *WorkspaceInfo
	}

	results := make(chan result, len(namespaces.Items))
	var wg sync.WaitGroup

	for _, ns := range namespaces.Items {
		wg.Add(1)
		go func(ns corev1.Namespace) {
			defer wg.Done()

			// Try to access the namespace by listing resource quotas (with retry)
			var quotas *corev1.ResourceQuotaList
			var err error
			for i := 0; i < 2; i++ {
				quotas, err = c.Clientset.CoreV1().ResourceQuotas(ns.Name).List(ctx, metav1.ListOptions{})
				if err == nil {
					break
				}
			}
			if err != nil {
				// User doesn't have access to this workspace
				results <- result{nil}
				return
			}

			info := &WorkspaceInfo{
				Name: ns.Name,
			}

			// Parse node selector annotation for node group
			if selector, ok := ns.Annotations[AnnotationNodeSelector]; ok {
				// Format: node-restriction.kubernetes.io/nodegroup=graduate
				if parts := strings.Split(selector, "="); len(parts) == 2 {
					info.NodeGroup = parts[1]
				}
			}

			// Parse quotas
			for _, quota := range quotas.Items {
				if gpuHard, ok := quota.Spec.Hard["requests.nvidia.com/gpu"]; ok {
					info.GPUQuota = gpuHard.Value()
				}
				if cpuHard, ok := quota.Spec.Hard["limits.cpu"]; ok {
					info.CPUQuota = cpuHard.String()
				}
				if memHard, ok := quota.Spec.Hard["limits.memory"]; ok {
					info.MemQuota = memHard.String()
				}
			}

			results <- result{info}
		}(ns)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var workspaces []WorkspaceInfo
	for r := range results {
		if r.info != nil {
			workspaces = append(workspaces, *r.info)
		}
	}

	return workspaces, nil
}

// Get returns information about a specific workspace
func Get(ctx context.Context, c *client.Client, name string) (*WorkspaceInfo, error) {
	// Get namespace with retry
	var ns *corev1.Namespace
	var err error
	for i := 0; i < 2; i++ {
		ns, err = c.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("workspace not found: %s", name)
	}

	// Check if it's an SGS workspace
	if _, ok := ns.Labels[LabelWorkspaceID]; !ok {
		return nil, fmt.Errorf("not an SGS workspace: %s", name)
	}

	// Try to access resource quotas to verify permission (with retry)
	var quotas *corev1.ResourceQuotaList
	for i := 0; i < 2; i++ {
		quotas, err = c.Clientset.CoreV1().ResourceQuotas(name).List(ctx, metav1.ListOptions{})
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("access denied to workspace: %s", name)
	}

	info := &WorkspaceInfo{
		Name: name,
	}

	// Parse node selector annotation
	if selector, ok := ns.Annotations[AnnotationNodeSelector]; ok {
		if parts := strings.Split(selector, "="); len(parts) == 2 {
			info.NodeGroup = parts[1]
		}
	}

	// Parse quotas
	for _, quota := range quotas.Items {
		if gpuHard, ok := quota.Spec.Hard["requests.nvidia.com/gpu"]; ok {
			info.GPUQuota = gpuHard.Value()
		}
		if cpuHard, ok := quota.Spec.Hard["limits.cpu"]; ok {
			info.CPUQuota = cpuHard.String()
		}
		if memHard, ok := quota.Spec.Hard["limits.memory"]; ok {
			info.MemQuota = memHard.String()
		}
	}

	return info, nil
}

// GetCurrent returns information about the current workspace
func GetCurrent(ctx context.Context, c *client.Client) (*WorkspaceInfo, error) {
	return Get(ctx, c, c.Namespace)
}
