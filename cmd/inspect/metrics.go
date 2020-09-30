package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/api/core/v1"
)

func recordMetrics() {
	go func() {
		for {
			var nodeName string
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
			gpuMemoryPodAllocated.Reset()
			writeMetricToGaugeVec(nodeInfos)
			time.Sleep(15 * time.Second)
		}
	}()
}

var (
	gpuMemoryPodAllocated = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "aliyun_com",
			Subsystem: "gpushare",
			Name:      "memory_allocated",
			Help:      "How much memory was allocated for a pod, memory unit .",
		},
		[]string{
			// Collect by node hostname and ip address
			"kubernetes_node_name",
			"kubernetes_node_ip",
			// Kubernetes Namespace
			"kubernetes_pod_namespace",
			// Container pod name
			"kubernetes_pod_name",
			// GPU Device ID
			"gpu_device_id",
			//
		},
	)
)

func init() {
	// Metrics have to be registered to be exposed:
	prometheus.MustRegister(gpuMemoryPodAllocated)
}

func exposeMetrics(listenPort int) {
	recordMetrics()
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(listenPort), nil))
}

func writeMetricToGaugeVec(nodeInfos []*NodeInfo) {
	var (
		gpuDeviceID string
	)

	for _, nodeInfo := range nodeInfos {
		address := "unknown"
		if len(nodeInfo.node.Status.Addresses) > 0 {
			//address = nodeInfo.node.Status.Addresses[0].Address
			for _, addr := range nodeInfo.node.Status.Addresses {
				if addr.Type == v1.NodeInternalIP {
					address = addr.Address
					break
				}
			}
		}
		usedGPUMemInNode := 0

		var buffer bytes.Buffer
		for i, dev := range nodeInfo.devs {
			usedGPUMemInNode += dev.usedGPUMem
			for _, pod := range dev.pods {

				buffer.WriteString(fmt.Sprintf("%s\t%s\t", pod.Name, pod.Namespace))
				count := nodeInfo.gpuCount
				if nodeInfo.hasPendingGPUMemory() {
					count++
				}

				for k := 0; k < count; k++ {
					if k == i || (i == -1 && k == nodeInfo.gpuCount) {
						gpuDeviceID = "GPU " + strconv.Itoa(k)
					} else {
						continue
						//gpuDeviceID = ""
						// buffer.WriteString("0\t")
					}
					gpuMemoryPodAllocated.WithLabelValues(nodeInfo.node.Name, address, pod.Namespace, pod.Name, gpuDeviceID).Set(float64(getGPUMemoryInPod(pod)))
					fmt.Println(nodeInfo.node.Name, address, pod.Namespace, pod.Name, gpuDeviceID, float64(getGPUMemoryInPod(pod)))
				}
				buffer.WriteString("\n")
			}
		}
	}
}
