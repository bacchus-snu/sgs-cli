package node

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceInfo holds resource usage information for a node
type ResourceInfo struct {
	CPUUsed      string
	CPUTotal     string
	MemoryUsed   string
	MemoryTotal  string
	GPUUsed      int64
	GPUTotal     int64
	StorageUsed  string
	StorageTotal string
	Group        string // node group from node-restriction.kubernetes.io/nodegroup label
}

// ListWorkerNodes returns all worker nodes (excludes control plane nodes)
func ListWorkerNodes(ctx context.Context, c *client.Client) ([]corev1.Node, error) {
	nodeList, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var workerNodes []corev1.Node
	for _, node := range nodeList.Items {
		// Skip control plane nodes
		if _, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]; isControlPlane {
			continue
		}
		// Also check for the older master label
		if _, isMaster := node.Labels["node-role.kubernetes.io/master"]; isMaster {
			continue
		}
		workerNodes = append(workerNodes, node)
	}

	return workerNodes, nil
}

// GetResourceInfo returns resource usage information for a specific node
func GetResourceInfo(ctx context.Context, c *client.Client, nodeName string) (*ResourceInfo, error) {
	// Get node
	node, err := c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Get allocatable resources
	allocatable := node.Status.Allocatable
	cpuTotal := allocatable.Cpu()
	memTotal := allocatable.Memory()
	gpuTotal := allocatable[corev1.ResourceName("nvidia.com/gpu")]

	// Get node group from label
	group := node.Labels["node-restriction.kubernetes.io/nodegroup"]
	if group == "" {
		group = "-"
	}

	// Get pods on this node to calculate usage
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Sum up requested resources from all pods
	cpuUsed := resource.NewQuantity(0, resource.DecimalSI)
	memUsed := resource.NewQuantity(0, resource.BinarySI)
	var gpuUsed int64

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Resources.Requests != nil {
				if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
					cpuUsed.Add(cpu)
				}
				if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
					memUsed.Add(mem)
				}
				if gpu, ok := container.Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]; ok {
					gpuUsed += gpu.Value()
				}
			}
		}
	}

	return &ResourceInfo{
		CPUUsed:      cpuUsed.String(),
		CPUTotal:     cpuTotal.String(),
		MemoryUsed:   formatMemory(memUsed),
		MemoryTotal:  formatMemory(memTotal),
		GPUUsed:      gpuUsed,
		GPUTotal:     gpuTotal.Value(),
		StorageUsed:  "N/A",
		StorageTotal: "N/A",
		Group:        group,
	}, nil
}

// formatMemory converts memory quantity to human-readable format
func formatMemory(q *resource.Quantity) string {
	bytes := q.Value()
	const (
		gi = 1024 * 1024 * 1024
		mi = 1024 * 1024
	)

	if bytes >= gi {
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(gi))
	} else if bytes >= mi {
		return fmt.Sprintf("%.1fMi", float64(bytes)/float64(mi))
	}
	return q.String()
}
