package main

import (
	"fmt"
	"strconv"

	log "github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/api/core/v1"
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
		fmt.Sprintf("%d", d.usedGPUMem)
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
	val, ok := node.Status.Capacity[resourceName]

	if !ok {
		return 0
	}

	return int(val.Value())
}

func getGPUCountInNode(node v1.Node) int {
	val, ok := node.Status.Capacity[countName]

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

GPUSearchLoop:
	for _, pod := range n.pods {
		if gpuMemoryInPod(pod) <= 0 {
			continue GPUSearchLoop
		}

		devID, usedGPUMem := n.getDeivceInfo(pod)

		var dev *DeviceInfo
		ok := false
		if dev, ok = n.devs[devID]; !ok {
			totalGPUMem := 0
			if n.gpuCount > 0 {
				totalGPUMem = n.gpuTotalMemory / n.gpuCount
			}

			dev = &DeviceInfo{
				pods:        []v1.Pod{},
				idx:         devID,
				totalGPUMem: totalGPUMem,
				node:        n.node,
			}
			n.devs[devID] = dev
		}

		dev.usedGPUMem = dev.usedGPUMem + usedGPUMem
		dev.pods = append(dev.pods, pod)
	}

	return nil
}

func (n *NodeInfo) getDeivceInfo(pod v1.Pod) (devIdx int, gpuMemory int) {
	var err error
	id := -1

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

	return id, gpuMemoryInPod(pod)
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
