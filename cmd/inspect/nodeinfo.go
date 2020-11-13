package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	log "github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
)

type DeviceInfo struct {
	idx         int
	pods        []v1.Pod
	usedGPUMem  int
	totalGPUMem int
	node        v1.Node
}

func (d *DeviceInfo) String() string {
	if d.idx == -1 {
		return fmt.Sprintf("%d", d.usedGPUMem)
	}
	return fmt.Sprintf("%d/%d", d.usedGPUMem, d.totalGPUMem)
}

func (d *DeviceInfo) addGPUPod(pod v1.Pod) {
	if len(d.pods) == 0 {
		d.pods = []v1.Pod{}
	}
	d.pods = append(d.pods, pod)
}

type NodeInfo struct {
	pods           []v1.Pod
	node           v1.Node
	devs           map[int]*DeviceInfo
	gpuCount       int
	gpuTotalMemory int
	pluginPod      v1.Pod
}

// The key function
func buildAllNodeInfos(allPods []v1.Pod, nodes []v1.Node) ([]*NodeInfo, error) {
	nodeInfos := buildNodeInfoWithPods(allPods, nodes)
	for _, info := range nodeInfos {
		if info.gpuTotalMemory > 0 {
			setUnit(info.gpuTotalMemory, info.gpuCount)
			err := info.buildDeviceInfo()
			if err != nil {
				log.Warningf("Failed due to %v", err)
				continue
			}
		}
	}
	return nodeInfos, nil
}

func (n *NodeInfo) acquirePluginPod() v1.Pod {
	if n.pluginPod.Name == "" {
		for _, pod := range n.pods {
			if val, ok := pod.Labels[pluginComponentKey]; ok {
				if val == pluginComponentValue {
					n.pluginPod = pod
					break
				}
			}
		}
	}
	return n.pluginPod
}

func getTotalGPUMemory(node v1.Node) int {
	val, ok := node.Status.Allocatable[resourceName]

	if !ok {
		return 0
	}

	return int(val.Value())
}

func getGPUCountInNode(node v1.Node) int {
	val, ok := node.Status.Allocatable[countName]

	if !ok {
		return int(0)
	}

	return int(val.Value())
}

func buildNodeInfoWithPods(pods []v1.Pod, nodes []v1.Node) []*NodeInfo {
	nodeMap := map[string]*NodeInfo{}
	nodeList := []*NodeInfo{}

	for _, node := range nodes {
		var info *NodeInfo = &NodeInfo{}
		if value, ok := nodeMap[node.Name]; ok {
			info = value
		} else {
			nodeMap[node.Name] = info
			info.node = node
			info.pods = []v1.Pod{}
			info.gpuCount = getGPUCountInNode(node)
			info.gpuTotalMemory = getTotalGPUMemory(node)
			info.devs = map[int]*DeviceInfo{}

			for i := 0; i < info.gpuCount; i++ {
				dev := &DeviceInfo{
					pods:        []v1.Pod{},
					idx:         i,
					totalGPUMem: info.gpuTotalMemory / info.gpuCount,
					node:        info.node,
				}
				info.devs[i] = dev
			}

		}

		for _, pod := range pods {
			if pod.Spec.NodeName == node.Name {
				info.pods = append(info.pods, pod)
			}
		}
	}

	for _, v := range nodeMap {
		nodeList = append(nodeList, v)
	}
	return nodeList
}

func (n *NodeInfo) hasPendingGPUMemory() bool {
	_, found := n.devs[-1]
	return found
}

// Get used GPUs in checkpoint
func (n *NodeInfo) buildDeviceInfo() error {
	totalGPUMem := 0
	if n.gpuCount > 0 {
		totalGPUMem = n.gpuTotalMemory / n.gpuCount
	}
GPUSearchLoop:
	for _, pod := range n.pods {
		if gpuMemoryInPod(pod) <= 0 {
			continue GPUSearchLoop
		}
		for devID, usedGPUMem := range n.getDeivceInfo(pod) {
			if n.devs[devID] == nil {
				n.devs[devID] = &DeviceInfo{
					pods:        []v1.Pod{},
					idx:         devID,
					totalGPUMem: totalGPUMem,
					node:        n.node,
				}
			}
			n.devs[devID].usedGPUMem += usedGPUMem
			n.devs[devID].pods = append(n.devs[devID].pods, pod)
		}
	}
	return nil
}

func (n *NodeInfo) getDeivceInfo(pod v1.Pod) map[int]int {
	var err error
	id := -1
	allocation := map[int]int{}
	allocation = GetAllocation(&pod)
	if len(allocation) != 0 {
		return allocation
	}
	if len(pod.ObjectMeta.Annotations) > 0 {
		value, found := pod.ObjectMeta.Annotations[envNVGPUID]
		if found {
			id, err = strconv.Atoi(value)
			if err != nil {
				log.Warningf("Failed to parse dev id %s due to %v for pod %s in ns %s",
					value,
					err,
					pod.Name,
					pod.Namespace)
				id = -1
			}
		} else {
			log.Warningf("Failed to get dev id %s for pod %s in ns %s",
				pod.Name,
				pod.Namespace)
		}
	}
	allocation[id] = gpuMemoryInPod(pod)
	return allocation
}

func hasPendingGPUMemory(nodeInfos []*NodeInfo) (found bool) {
	for _, info := range nodeInfos {
		if info.hasPendingGPUMemory() {
			return true
		}
	}

	return false
}

func getNodes(nodeName string) ([]v1.Node, error) {
	node, err := clientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	return []v1.Node{*node}, err
}

func isGPUSharingNode(node v1.Node) bool {
	value, ok := node.Status.Allocatable[resourceName]

	if ok {
		ok = (int(value.Value()) > 0)
	}

	return ok
}

var (
	memoryUnit = ""
)

func setUnit(gpuMemory, gpuCount int) {
	if memoryUnit != "" {
		return
	}

	if gpuCount == 0 {
		return
	}

	gpuMemoryByDev := gpuMemory / gpuCount

	if gpuMemoryByDev > 100 {
		memoryUnit = "MiB"
	} else {
		memoryUnit = "GiB"
	}
}
func GetAllocation(pod *v1.Pod) map[int]int {
	podGPUMems := map[int]int{}
	allocationString := ""
	if pod.ObjectMeta.Annotations == nil {
		return podGPUMems
	}
	value, ok := pod.ObjectMeta.Annotations[gpushareAllocationFlag]
	if !ok {
		return podGPUMems
	}
	allocationString = value
	var allocation map[int]map[string]int
	err := json.Unmarshal([]byte(allocationString), &allocation)
	if err != nil {
		return podGPUMems
	}
	for _, containerAllocation := range allocation {
		for id, gpuMem := range containerAllocation {
			gpuIndex, err := strconv.Atoi(id)
			if err != nil {
				log.Errorf("failed to get gpu memory from pod annotation,reason: %v", err)
				return map[int]int{}
			}
			podGPUMems[gpuIndex] += gpuMem
		}
	}
	return podGPUMems
}
