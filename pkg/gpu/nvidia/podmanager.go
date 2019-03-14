package nvidia

import (
	"fmt"
	"os"
	"sort"
	"time"

	log "github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	nodeutil "k8s.io/kubernetes/pkg/util/node"
)

var (
	clientset *kubernetes.Clientset
	nodeName  string
	retries   = 5
)

func kubeInit() {
	kubeconfigFile := os.Getenv("KUBECONFIG")
	var err error
	var config *rest.Config

	if _, err = os.Stat(kubeconfigFile); err != nil {
		log.V(5).Infof("kubeconfig %s failed to find due to %v", kubeconfigFile, err)
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Failed due to %v", err)
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile)
		if err != nil {
			log.Fatalf("Failed due to %v", err)
		}
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}

	nodeName = os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatalln("Please set env NODE_NAME")
	}

}

func patchGPUCount(gpuCount int) error {
	node, err := clientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if val, ok := node.Status.Capacity[resourceCount]; ok {
		if val.Value() == int64(gpuCount) {
			log.Infof("No need to update Capacity %s", resourceCount)
			return nil
		}
	}

	newNode := node.DeepCopy()
	newNode.Status.Capacity[resourceCount] = *resource.NewQuantity(int64(gpuCount), resource.DecimalSI)
	newNode.Status.Allocatable[resourceCount] = *resource.NewQuantity(int64(gpuCount), resource.DecimalSI)

	for i := 0; i < gpuCount; i++ {
		newNode.Status.Allocatable[v1.ResourceName(fmt.Sprintf("%s%d", resourceStatus, i))] =
			*resource.NewQuantity(1, resource.DecimalSI)
	}
	// content := fmt.Sprintf(`[{"op": "add", "path": "/status/capacity/aliyun.com~gpu-count", "value": "%d"}]`, gpuCount)
	// _, err = clientset.CoreV1().Nodes().PatchStatus(nodeName, []byte(content))
	_, _, err = nodeutil.PatchNodeStatus(clientset.CoreV1(), types.NodeName(nodeName), node, newNode)
	if err != nil {
		log.Infof("Failed to update Capacity %s.", resourceCount)
	} else {
		log.Infof("Updated Capacity %s successfully.", resourceCount)
	}
	return err
}

func patchGPUUnhealthyStatus(array []uint) error {

	node, err := clientset.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	newNode := node.DeepCopy()
	for _, val := range array {
		newNode.Status.Allocatable[v1.ResourceName(fmt.Sprintf("%s%u", resourceStatus, val))] =
			*resource.NewQuantity(0, resource.DecimalSI)
	}
	_, _, err = nodeutil.PatchNodeStatus(clientset.CoreV1(), types.NodeName(nodeName), node, newNode)
	if err != nil {
		log.Infof("Failed to update GPU Card %s.", resourceStatus)
	} else {
		log.Infof("Updated Capacity %s successfully.", resourceStatus)
	}
	return err
}

func getPendingPodsInNode() ([]v1.Pod, error) {
	// pods, err := m.lister.List(labels.Everything())
	// if err != nil {
	// 	return nil, err
	// }
	pods := []v1.Pod{}

	podIDMap := map[types.UID]bool{}

	selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName, "status.phase": "Pending"})
	podList, err := clientset.CoreV1().Pods(v1.NamespaceAll).List(metav1.ListOptions{
		FieldSelector: selector.String(),
		LabelSelector: labels.Everything().String(),
	})
	for i := 0; i < retries && err != nil; i++ {
		podList, err = clientset.CoreV1().Pods(v1.NamespaceAll).List(metav1.ListOptions{
			FieldSelector: selector.String(),
			LabelSelector: labels.Everything().String(),
		})
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Pods assigned to node %v", nodeName)
	}

	log.V(5).Infof("all pod list %v", podList.Items)

	// if log.V(5) {
	for _, pod := range podList.Items {
		if pod.Spec.NodeName != nodeName {
			log.Warningf("Pod name %s in ns %s is not assigned to node %s as expected, it's placed on node %s ",
				pod.Name,
				pod.Namespace,
				nodeName,
				pod.Spec.NodeName)
		} else {
			log.Infof("list pod %s in ns %s in node %s and status is %s",
				pod.Name,
				pod.Namespace,
				nodeName,
				pod.Status.Phase,
			)
			if _, ok := podIDMap[pod.UID]; !ok {
				pods = append(pods, pod)
				podIDMap[pod.UID] = true
			}
		}

	}
	// }

	return pods, nil
}

// pick up the gpushare pod with assigned status is false, and
func getCandidatePods() ([]*v1.Pod, error) {
	candidatePods := []*v1.Pod{}
	allPods, err := getPendingPodsInNode()
	if err != nil {
		return candidatePods, err
	}
	for _, pod := range allPods {
		current := pod
		if isGPUMemoryAssumedPod(&current) {
			candidatePods = append(candidatePods, &current)
		}
	}

	if log.V(4) {
		for _, pod := range candidatePods {
			log.Infof("candidate pod %s in ns %s with timestamp %d is found.",
				pod.Name,
				pod.Namespace,
				getAssumeTimeFromPodAnnotation(pod))
		}
	}

	return makePodOrderdByAge(candidatePods), nil
}

// make the pod ordered by GPU assumed time
func makePodOrderdByAge(pods []*v1.Pod) []*v1.Pod {
	newPodList := make(orderedPodByAssumeTime, 0, len(pods))
	for _, v := range pods {
		newPodList = append(newPodList, v)
	}
	sort.Sort(newPodList)
	return []*v1.Pod(newPodList)
}

type orderedPodByAssumeTime []*v1.Pod

func (this orderedPodByAssumeTime) Len() int {
	return len(this)
}

func (this orderedPodByAssumeTime) Less(i, j int) bool {
	return getAssumeTimeFromPodAnnotation(this[i]) <= getAssumeTimeFromPodAnnotation(this[j])
}

func (this orderedPodByAssumeTime) Swap(i, j int) {
	this[i], this[j] = this[j], this[i]
}
