package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	log "github.com/golang/glog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func displayDetails(nodeInfos []*NodeInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	var (
		totalGPUMemInCluster int64
		usedGPUMemInCluster  int64
		prtLineLen           int
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

		totalGPUMemInNode := nodeInfo.gpuTotalMemory
		if totalGPUMemInNode <= 0 {
			continue
		}

		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "NAME:\t%s\n", nodeInfo.node.Name)
		fmt.Fprintf(w, "IPADDRESS:\t%s\n", address)
		fmt.Fprintf(w, "\n")

		usedGPUMemInNode := 0
		var buf bytes.Buffer
		buf.WriteString("NAME\tNAMESPACE\t")
		for i := 0; i < nodeInfo.gpuCount; i++ {
			buf.WriteString(fmt.Sprintf("GPU%d(Allocated)\t", i))
		}

		if nodeInfo.hasPendingGPUMemory() {
			buf.WriteString("Pending(Allocated)\t")
		}
		buf.WriteString("\n")
		fmt.Fprintf(w, buf.String())

		var buffer bytes.Buffer
		exists := map[types.UID]bool{}
		for i, dev := range nodeInfo.devs {
			usedGPUMemInNode += dev.usedGPUMem
			for _, pod := range dev.pods {
				if _,ok := exists[pod.UID]; ok {
					continue 
				}
				buffer.WriteString(fmt.Sprintf("%s\t%s\t", pod.Name, pod.Namespace))
				count := nodeInfo.gpuCount
				if nodeInfo.hasPendingGPUMemory() {
					count += 1
				}

				for k := 0; k < count; k++ {
					allocation := GetAllocation(&pod) 
					if len(allocation) != 0 {
						buffer.WriteString(fmt.Sprintf("%d\t", allocation[k]))
						continue 
					}
					if k == i || (i == -1 && k == nodeInfo.gpuCount) {
						buffer.WriteString(fmt.Sprintf("%d\t", getGPUMemoryInPod(pod)))
					} else {
						buffer.WriteString("0\t")
					}
				}
				buffer.WriteString("\n")
				exists[pod.UID] = true 
			}
		}
		if prtLineLen == 0 {
			prtLineLen = buffer.Len() + 10
		}
		fmt.Fprintf(w, buffer.String())

		var gpuUsageInNode float64 = 0
		if totalGPUMemInNode > 0 {
			gpuUsageInNode = float64(usedGPUMemInNode) / float64(totalGPUMemInNode) * 100
		} else {
			fmt.Fprintf(w, "\n")
		}

		fmt.Fprintf(w, "Allocated :\t%d (%d%%)\t\n", usedGPUMemInNode, int64(gpuUsageInNode))
		fmt.Fprintf(w, "Total :\t%d \t\n", nodeInfo.gpuTotalMemory)
		// fmt.Fprintf(w, "-----------------------------------------------------------------------------------------\n")
		var prtLine bytes.Buffer
		for i := 0; i < prtLineLen; i++ {
			prtLine.WriteString("-")
		}
		prtLine.WriteString("\n")
		fmt.Fprintf(w, prtLine.String())
		totalGPUMemInCluster += int64(totalGPUMemInNode)
		usedGPUMemInCluster += int64(usedGPUMemInNode)
	}
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "Allocated/Total GPU Memory In Cluster:\t")
	log.V(2).Infof("gpu: %s, allocated GPU Memory %s", strconv.FormatInt(totalGPUMemInCluster, 10),
		strconv.FormatInt(usedGPUMemInCluster, 10))

	var gpuUsage float64 = 0
	if totalGPUMemInCluster > 0 {
		gpuUsage = float64(usedGPUMemInCluster) / float64(totalGPUMemInCluster) * 100
	}
	fmt.Fprintf(w, "%s/%s (%d%%)\t\n",
		strconv.FormatInt(usedGPUMemInCluster, 10),
		strconv.FormatInt(totalGPUMemInCluster, 10),
		int64(gpuUsage))
	// fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ...)

	_ = w.Flush()
}

func getMaxGPUCount(nodeInfos []*NodeInfo) (max int) {
	for _, node := range nodeInfos {
		if node.gpuCount > max {
			max = node.gpuCount
		}
	}

	return max
}

func displaySummary(nodeInfos []*NodeInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	var (
		maxGPUCount          int
		totalGPUMemInCluster int64
		usedGPUMemInCluster  int64
		prtLineLen           int
	)

	hasPendingGPU := hasPendingGPUMemory(nodeInfos)

	maxGPUCount = getMaxGPUCount(nodeInfos)

	var buffer bytes.Buffer
	buffer.WriteString("NAME\tIPADDRESS\t")
	for i := 0; i < maxGPUCount; i++ {
		buffer.WriteString(fmt.Sprintf("GPU%d(Allocated/Total)\t", i))
	}

	if hasPendingGPU {
		buffer.WriteString("PENDING(Allocated)\t")
	}
	buffer.WriteString(fmt.Sprintf("GPU Memory(%s)\n", memoryUnit))

	// fmt.Fprintf(w, "NAME\tIPADDRESS\tROLE\tGPU(Allocated/Total)\tPENDING(Allocated)\n")
	fmt.Fprintf(w, buffer.String())
	for _, nodeInfo := range nodeInfos {
		address := "unknown"
		if len(nodeInfo.node.Status.Addresses) > 0 {
			// address = nodeInfo.node.Status.Addresses[0].Address
			for _, addr := range nodeInfo.node.Status.Addresses {
				if addr.Type == v1.NodeInternalIP {
					address = addr.Address
					break
				}
			}
		}

		gpuMemInfos := []string{}
		pendingGPUMemInfo := ""
		usedGPUMemInNode := 0
		totalGPUMemInNode := nodeInfo.gpuTotalMemory
		if totalGPUMemInNode <= 0 {
			continue
		}

		for i := 0; i < maxGPUCount; i++ {
			gpuMemInfo := "0/0"
			if dev, ok := nodeInfo.devs[i]; ok {
				gpuMemInfo = dev.String()
				usedGPUMemInNode += dev.usedGPUMem
			}
			gpuMemInfos = append(gpuMemInfos, gpuMemInfo)
		}

		// check if there is pending dev
		if dev, ok := nodeInfo.devs[-1]; ok {
			pendingGPUMemInfo = fmt.Sprintf("%d", dev.usedGPUMem)
			usedGPUMemInNode += dev.usedGPUMem
		}

		nodeGPUMemInfo := fmt.Sprintf("%d/%d", usedGPUMemInNode, totalGPUMemInNode)

		var buf bytes.Buffer
		buf.WriteString(fmt.Sprintf("%s\t%s\t", nodeInfo.node.Name, address))
		for i := 0; i < maxGPUCount; i++ {
			buf.WriteString(fmt.Sprintf("%s\t", gpuMemInfos[i]))
		}
		if hasPendingGPU {
			buf.WriteString(fmt.Sprintf("%s\t", pendingGPUMemInfo))
		}

		buf.WriteString(fmt.Sprintf("%s\n", nodeGPUMemInfo))
		fmt.Fprintf(w, buf.String())

		if prtLineLen == 0 {
			prtLineLen = buf.Len() + 20
		}

		usedGPUMemInCluster += int64(usedGPUMemInNode)
		totalGPUMemInCluster += int64(totalGPUMemInNode)
	}
	// fmt.Fprintf(w, "-----------------------------------------------------------------------------------------\n")
	var prtLine bytes.Buffer
	for i := 0; i < prtLineLen; i++ {
		prtLine.WriteString("-")
	}
	prtLine.WriteString("\n")
	fmt.Fprint(w, prtLine.String())

	fmt.Fprintf(w, "Allocated/Total GPU Memory In Cluster:\n")
	log.V(2).Infof("gpu: %s, allocated GPU Memory %s", strconv.FormatInt(totalGPUMemInCluster, 10),
		strconv.FormatInt(usedGPUMemInCluster, 10))
	var gpuUsage float64 = 0
	if totalGPUMemInCluster > 0 {
		gpuUsage = float64(usedGPUMemInCluster) / float64(totalGPUMemInCluster) * 100
	}
	fmt.Fprintf(w, "%s/%s (%d%%)\t\n",
		strconv.FormatInt(usedGPUMemInCluster, 10),
		strconv.FormatInt(totalGPUMemInCluster, 10),
		int64(gpuUsage))
	// fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ...)

	_ = w.Flush()
}

func getGPUMemoryInPod(pod v1.Pod) int {
	gpuMem := 0
	for _, container := range pod.Spec.Containers {
		if val, ok := container.Resources.Limits[resourceName]; ok {
			gpuMem += int(val.Value())
		}
	}
	return gpuMem
}
