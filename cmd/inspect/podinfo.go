package main

import (
	"fmt"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"os"
	"path"
	"time"

	log "github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientConfig clientcmd.ClientConfig
	clientset    *kubernetes.Clientset
	restConfig   *rest.Config
	retries      = 5
)

func kubeInit() {

	kubeconfigFile := os.Getenv("KUBECONFIG")
	if kubeconfigFile == "" {
		kubeconfigFile = path.Join(os.Getenv("HOME"), "/.kube/config")
	}
	if _, err := os.Stat(kubeconfigFile); err != nil {
		log.Fatalf("kubeconfig %s failed to find due to %v, please set KUBECONFIG env", kubeconfigFile, err)
	}

	var err error
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile)
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}
	clientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}
}

type podInfo struct {
	name      string
	namespace string
}

func (p podInfo) equal(p1 podInfo) bool {
	return p.name == p1.name && p.namespace == p1.namespace
}

func getActivePodsByNode(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName})
	pods, err := clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: selector.String(),
		LabelSelector: labels.Everything().String(),
	})

	for i := 0; i < retries && err != nil; i++ {
		pods, err = clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
			FieldSelector: selector.String(),
			LabelSelector: labels.Everything().String(),
		})
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return []v1.Pod{}, fmt.Errorf("failed to get Pods in node %v", nodeName)
	}

	return filterActivePods(pods.Items), nil
}

func getActivePodsInAllNodes(ctx context.Context) ([]v1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	})

	for i := 0; i < retries && err != nil; i++ {
		pods, err = clientset.CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{
			LabelSelector: labels.Everything().String(),
		})
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return []v1.Pod{}, fmt.Errorf("failed to get Pods")
	}
	return filterActivePods(pods.Items), nil
}

func filterActivePods(pods []v1.Pod) (activePods []v1.Pod) {
	activePods = []v1.Pod{}
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		}

		activePods = append(activePods, pod)
	}

	return activePods
}

func getAllSharedGPUNode(ctx context.Context) ([]v1.Node, error) {
	nodes := []v1.Node{}
	allNodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nodes, err
	}

	for _, item := range allNodes.Items {
		if isGPUSharingNode(item) {
			nodes = append(nodes, item)
		}
	}

	return nodes, nil
}

func gpuMemoryInPod(pod v1.Pod) int {
	var total int
	containers := pod.Spec.Containers
	for _, container := range containers {
		if val, ok := container.Resources.Limits[resourceName]; ok {
			total += int(val.Value())
		}
	}

	return total
}
