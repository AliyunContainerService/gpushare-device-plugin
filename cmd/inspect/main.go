package main

import (
	"flag"
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
)

const (
	resourceName         = "aliyun.com/gpu-mem"
	countName            = "aliyun.com/gpu-count"
	gpuCountKey          = "aliyun.accelerator/nvidia_count"
	cardNameKey          = "aliyun.accelerator/nvidia_name"
	gpuMemKey            = "aliyun.accelerator/nvidia_mem"
	pluginComponentKey   = "component"
	pluginComponentValue = "gpushare-device-plugin"

	envNVGPUID        = "ALIYUN_COM_GPU_MEM_IDX"
	envPodGPUMemory   = "ALIYUN_COM_GPU_MEM_POD"
	envTOTALGPUMEMORY = "ALIYUN_COM_GPU_MEM_DEV"
)

func init() {
	kubeInit()
	// checkpointInit()
}

func main() {
	var nodeName string
	// nodeName := flag.String("nodeName", "", "nodeName")
	details := flag.Bool("d", false, "details")
	metrics := flag.Bool("m", false, "metrics")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 {
		nodeName = args[0]
	}

	var pods []v1.Pod
	var nodes []v1.Node
	var err error

	if nodeName == "" {
		nodes, err = getAllSharedGPUNode()
		if err == nil {
			pods, err = getActivePodsInAllNodes()
		}
	} else {
		nodes, err = getNodes(nodeName)
		if err == nil {
			pods, err = getActivePodsByNode(nodeName)
		}
	}

	if err != nil {
		fmt.Printf("Failed due to %v", err)
		os.Exit(1)
	}

	nodeInfos, err := buildAllNodeInfos(pods, nodes)
	if err != nil {
		fmt.Printf("Failed due to %v", err)
		os.Exit(1)
	}
	if *details {
		displayDetails(nodeInfos)
	} else if *metrics {
		exposeMetrics()
	} else {
		displaySummary(nodeInfos)
	}
}
