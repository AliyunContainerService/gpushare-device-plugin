package nvidia

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	log "github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
)

// update pod env with assigned status
func updatePodAnnotations(oldPod *v1.Pod) (newPod *v1.Pod) {
	newPod = oldPod.DeepCopy()
	if len(newPod.ObjectMeta.Annotations) == 0 {
		newPod.ObjectMeta.Annotations = map[string]string{}
	}

	now := time.Now()
	newPod.ObjectMeta.Annotations[EnvAssignedFlag] = "true"
	newPod.ObjectMeta.Annotations[EnvResourceAssumeTime] = fmt.Sprintf("%d", now.UnixNano())

	return newPod
}

func patchPodAnnotationSpecAssigned() ([]byte, error) {
	now := time.Now()
	patchAnnotations := map[string]interface{}{
		"metadata": map[string]map[string]string{"annotations": {
			EnvAssignedFlag:       "true",
			EnvResourceAssumeTime: fmt.Sprintf("%d", now.UnixNano()),
		}}}
	return json.Marshal(patchAnnotations)
}

func getGPUIDFromPodAnnotation(pod *v1.Pod) (id int) {
	var err error
	id = -1

	if len(pod.ObjectMeta.Annotations) > 0 {
		value, found := pod.ObjectMeta.Annotations[EnvResourceIndex]
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
				value,
				pod.Name,
				pod.Namespace)
		}
	}

	return id
}

// get assumed timestamp
func getAssumeTimeFromPodAnnotation(pod *v1.Pod) (assumeTime uint64) {
	if assumeTimeStr, ok := pod.ObjectMeta.Annotations[EnvResourceAssumeTime]; ok {
		u64, err := strconv.ParseUint(assumeTimeStr, 10, 64)
		if err != nil {
			log.Warningf("Failed to parse assume Timestamp %s due to %v", assumeTimeStr, err)
		} else {
			assumeTime = u64
		}
	}

	return assumeTime
}

// determine if the pod is GPU share pod, and is already assumed but not assigned
func isGPUMemoryAssumedPod(pod *v1.Pod) (assumed bool) {
	log.V(6).Infof("Determine if the pod %v is GPUSharedAssumed pod", pod)
	var ok bool

	// 1. Check if it's for GPU share
	if getGPUMemoryFromPodResource(pod) <= 0 {
		log.V(6).Infof("Pod %s in namespace %s has not GPU Memory Request, so it's not GPUSharedAssumed assumed pod.",
			pod.Name,
			pod.Namespace)
		return assumed
	}

	// 2. Check if it already has assume time
	if _, ok = pod.ObjectMeta.Annotations[EnvResourceAssumeTime]; !ok {
		log.V(4).Infof("No assume timestamp for pod %s in namespace %s, so it's not GPUSharedAssumed assumed pod.",
			pod.Name,
			pod.Namespace)
		return assumed
	}

	// 3. Check if it has been assigned already
	if assigned, ok := pod.ObjectMeta.Annotations[EnvAssignedFlag]; ok {

		if assigned == "false" {
			log.V(4).Infof("Found GPUSharedAssumed assumed pod %s in namespace %s.",
				pod.Name,
				pod.Namespace)
			assumed = true
		} else {
			log.Infof("GPU assigned Flag for pod %s exists in namespace %s and its assigned status is %s, so it's not GPUSharedAssumed assumed pod.",
				pod.Name,
				pod.Namespace,
				assigned)
		}
	} else {
		log.Warningf("No GPU assigned Flag for pod %s in namespace %s, so it's not GPUSharedAssumed assumed pod.",
			pod.Name,
			pod.Namespace)
	}

	return assumed
}

// Get GPU Memory of the Pod
func getGPUMemoryFromPodResource(pod *v1.Pod) uint {
	var total uint
	containers := pod.Spec.Containers
	for _, container := range containers {
		if val, ok := container.Resources.Limits[resourceName]; ok {
			total += uint(val.Value())
		}
	}
	return total
}

func podIsNotRunning(pod v1.Pod) bool {
	status := pod.Status
	//deletionTimestamp
	if pod.DeletionTimestamp != nil {
		return true
	}

	// pod is scheduled but not initialized
	if status.Phase == v1.PodPending && podConditionTrueOnly(status.Conditions, v1.PodScheduled) {
		log.Infof("Pod %s only has PodScheduled, is not running", pod.Name)
		return true
	}

	return status.Phase == v1.PodFailed || status.Phase == v1.PodSucceeded || (pod.DeletionTimestamp != nil && notRunning(status.ContainerStatuses)) || (status.Phase == v1.PodPending && podConditionTrueOnly(status.Conditions, v1.PodScheduled))
}

// notRunning returns true if every status is terminated or waiting, or the status list
// is empty.
func notRunning(statuses []v1.ContainerStatus) bool {
	for _, status := range statuses {
		if status.State.Terminated == nil && status.State.Waiting == nil {
			return false
		}
	}
	return true
}

func podConditionTrue(conditions []v1.PodCondition, expect v1.PodConditionType) bool {
	for _, condition := range conditions {
		if condition.Type == expect && condition.Status == v1.ConditionTrue {
			return true
		}
	}

	return false
}

func podConditionTrueOnly(conditions []v1.PodCondition, expect v1.PodConditionType) bool {
	if len(conditions) != 1 {
		return false
	}

	for _, condition := range conditions {
		if condition.Type == expect && condition.Status == v1.ConditionTrue {
			return true
		}
	}

	return false
}
