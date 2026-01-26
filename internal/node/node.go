// Package node provides functionality for managing Kubernetes nodes
// within the SGS (Sommelier GPU System) environment. It handles node
// listing, resource queries, and GPU availability checks.
package node

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HamiAnnotationKey is the HAMi annotation for GPU info (count, type, memory)
const HamiAnnotationKey = "hami.io/node-nvidia-register"

// HamiGPUInfo represents a single GPU entry from HAMi annotation
type HamiGPUInfo struct {
	ID      string `json:"id"`
	Count   int    `json:"count"`  // vGPU count (virtual, ignore for physical count)
	DevMem  int64  `json:"devmem"` // GPU memory in MiB (physical when deviceMemoryScaling=1)
	DevCore int    `json:"devcore"`
	Type    string `json:"type"`
	Health  bool   `json:"health"`
}

// ResourceInfo holds resource usage information for a node
type ResourceInfo struct {
	// CPU metrics (in cores)
	CPUAlloc    float64 // sum of pod limits
	CPUCapacity float64 // node allocatable

	// Host Memory metrics (in GiB)
	MemAlloc    float64 // sum of pod limits
	MemCapacity float64 // node allocatable

	// GPU metrics
	GPUAlloc    int64  // allocated vGPU count (sum of pod limits)
	GPUCapacity int64  // physical GPU count
	GPUType     string // GPU type (e.g., "NVIDIA GeForce GTX 1080")

	// GPU Memory metrics (in GiB)
	GPUMemAlloc    float64 // sum of pod limits (in GiB)
	GPUMemCapacity float64 // physical total (in GiB)

	// Node group
	Group string // node group from node-restriction.kubernetes.io/nodegroup label
}

// ListWorkerNodes returns all worker nodes (excludes control plane nodes)
func ListWorkerNodes(ctx context.Context, c *client.Client) ([]corev1.Node, error) {
	nodeList, err := client.RetryWithContext(ctx, func() (*corev1.NodeList, error) {
		return c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "list", "nodes", "cluster")
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
	node, err := client.RetryWithContext(ctx, func() (*corev1.Node, error) {
		return c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "get", "node", "cluster")
	}

	// Get allocatable resources (capacity)
	allocatable := node.Status.Allocatable
	cpuCapacity := cpuToFloat(allocatable.Cpu())
	memCapacity := memoryToGiB(allocatable.Memory())

	// Get GPU count, type, and memory from HAMi annotation
	gpuCapacity, gpuType, gpuMemMiB := parseHamiAnnotation(node)
	gpuMemCapacity := float64(gpuMemMiB) / 1024.0 // MiB to GiB

	// Get node group from label
	group := node.Labels["node-restriction.kubernetes.io/nodegroup"]
	if group == "" {
		group = "-"
	}

	// Get pods on this node to calculate allocated resources
	pods, err := client.RetryWithContext(ctx, func() (*corev1.PodList, error) {
		return c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", nodeName),
		})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "list", "pods", "cluster")
	}

	// Sum up resource limits from all pods (allocated resources)
	// Note: We use limits (not requests) because SGS supports CPU/memory oversubscription.
	cpuAlloc := resource.NewQuantity(0, resource.DecimalSI)
	memAlloc := resource.NewQuantity(0, resource.BinarySI)
	var gpuAlloc int64
	var gpuMemAllocMiB int64

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			// Use Limits for CPU and memory (oversubscription supported)
			if container.Resources.Limits != nil {
				if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
					cpuAlloc.Add(cpu)
				}
				if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
					memAlloc.Add(mem)
				}
				// GPU count from limits (vGPU allocation)
				if gpu, ok := container.Resources.Limits[corev1.ResourceName("nvidia.com/gpu")]; ok {
					gpuAlloc += gpu.Value()
				}
				// GPU memory from limits (in MiB)
				if gpuMem, ok := container.Resources.Limits[corev1.ResourceName("nvidia.com/gpumem")]; ok {
					gpuMemAllocMiB += gpuMem.Value()
				}
			}
		}
	}

	return &ResourceInfo{
		CPUAlloc:    cpuToFloat(cpuAlloc),
		CPUCapacity: cpuCapacity,

		MemAlloc:    memoryToGiB(memAlloc),
		MemCapacity: memCapacity,

		GPUAlloc:       gpuAlloc,
		GPUCapacity:    gpuCapacity,
		GPUType:        gpuType,
		GPUMemAlloc:    float64(gpuMemAllocMiB) / 1024.0, // MiB to GiB
		GPUMemCapacity: gpuMemCapacity,

		Group: group,
	}, nil
}

// cpuToFloat converts a CPU quantity to float64 cores
func cpuToFloat(q *resource.Quantity) float64 {
	return float64(q.MilliValue()) / 1000.0
}

// memoryToGiB converts a memory quantity to GiB as float64
func memoryToGiB(q *resource.Quantity) float64 {
	const gi = 1024 * 1024 * 1024
	return float64(q.Value()) / float64(gi)
}

// parseHamiAnnotation parses the HAMi GPU annotation and returns GPU count, type, and total memory
func parseHamiAnnotation(node *corev1.Node) (gpuCount int64, gpuType string, gpuMemMiB int64) {
	annotation := node.Annotations[HamiAnnotationKey]
	if annotation == "" {
		return 0, "", 0
	}

	var gpus []HamiGPUInfo
	if err := json.Unmarshal([]byte(annotation), &gpus); err != nil {
		return 0, "", 0
	}

	gpuCount = int64(len(gpus))
	for _, gpu := range gpus {
		if gpuType == "" && gpu.Type != "" {
			gpuType = gpu.Type
		}
		gpuMemMiB += gpu.DevMem // Sum memory from all GPUs
	}
	return
}
