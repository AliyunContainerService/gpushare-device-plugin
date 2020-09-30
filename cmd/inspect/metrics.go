package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/api/core/v1"
)

func recordMetrics() {
	go func() {
		for {
			//gpuMemoryPodAllocated.With(prometheus.Labels{"type": "delete", "user": "alice"}).Inc()
			gpuMemoryPodAllocated.WithLabelValues("za-zte-k8s-gpu-62.10", "10.50.62.10", "zcommmedia", "gpu-resize", "gpu1").Set(2)
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
			//ConstLabels: prometheus.Labels{"binary_version": "123"},
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

func exposeMetrics() {
	recordMetrics()
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":2112", nil))
}

func writeMetricToGaugeVec(nodeInfos []*NodeInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	var (
		// totalGPUMemInCluster int64
		// usedGPUMemInCluster int64
		gpuDeviceID string
		// prtLineLen  int
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

		// totalGPUMemInNode := nodeInfo.gpuTotalMemory
		// if totalGPUMemInNode <= 0 {
		// 	continue
		// }

		// fmt.Fprintf(w, "\n")
		// fmt.Fprintf(w, "NAME:\t%s\n", nodeInfo.node.Name)
		// fmt.Fprintf(w, "IPADDRESS:\t%s\n", address)
		// fmt.Fprintf(w, "\n")

		usedGPUMemInNode := 0
		// var buf bytes.Buffer
		// buf.WriteString("NAME\tNAMESPACE\t")
		// for i := 0; i < nodeInfo.gpuCount; i++ {
		// 	buf.WriteString(fmt.Sprintf("GPU%d(Allocated)\t", i))
		// }

		// if nodeInfo.hasPendingGPUMemory() {
		// 	buf.WriteString("Pending(Allocated)\t")
		// }
		// buf.WriteString("\n")
		// fmt.Fprintf(w, buf.String())

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
						gpuDeviceID = strconv.FormatInt(int64(k), 10)
						// buffer.WriteString(fmt.Sprintf("%d\t", getGPUMemoryInPod(pod)))
					} else {
						gpuDeviceID = ""
						// buffer.WriteString("0\t")
					}
					gpuMemoryPodAllocated.WithLabelValues(nodeInfo.node.Name, address, pod.Namespace, pod.Name, gpuDeviceID).Set(float64(getGPUMemoryInPod(pod)))
				}
				buffer.WriteString("\n")
			}
		}
		// if prtLineLen == 0 {
		// 	prtLineLen = buffer.Len() + 10
		// }
		// fmt.Fprintf(w, buffer.String())

		// var gpuUsageInNode float64 = 0
		// if totalGPUMemInNode > 0 {
		// 	gpuUsageInNode = float64(usedGPUMemInNode) / float64(totalGPUMemInNode) * 100
		// } else {
		// 	fmt.Fprintf(w, "\n")
		// }

		// fmt.Fprintf(w, "Allocated :\t%d (%d%%)\t\n", usedGPUMemInNode, int64(gpuUsageInNode))
		// fmt.Fprintf(w, "Total :\t%d \t\n", nodeInfo.gpuTotalMemory)
		// // fmt.Fprintf(w, "-----------------------------------------------------------------------------------------\n")
		// var prtLine bytes.Buffer
		// for i := 0; i < prtLineLen; i++ {
		// 	prtLine.WriteString("-")
		// }
		// prtLine.WriteString("\n")
		// fmt.Fprintf(w, prtLine.String())
		//		totalGPUMemInCluster += int64(totalGPUMemInNode)
		//		usedGPUMemInCluster += int64(usedGPUMemInNode)
		// }
		// fmt.Fprintf(w, "\n")
		// fmt.Fprintf(w, "\n")
		// fmt.Fprintf(w, "Allocated/Total GPU Memory In Cluster:\t")
		//log.V(2).Infof("gpu: %s, allocated GPU Memory %s", strconv.FormatInt(totalGPUMemInCluster, 10),strconv.FormatInt(usedGPUMemInCluster, 10))

		// var gpuUsage float64 = 0
		// if totalGPUMemInCluster > 0 {
		// 	gpuUsage = float64(usedGPUMemInCluster) / float64(totalGPUMemInCluster) * 100
		// }
		// fmt.Fprintf(w, "%s/%s (%d%%)\t\n",
		// 	strconv.FormatInt(usedGPUMemInCluster, 10),
		// 	strconv.FormatInt(totalGPUMemInCluster, 10),
		// 	int64(gpuUsage))
		// fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ...)

		_ = w.Flush()
	}
}
