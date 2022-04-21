package nvidia

import (
	"fmt"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

var (
	clientTimeout    = 30 * time.Second
	lastAllocateTime time.Time
)

// create docker client
func init() {
	kubeInit()
}

func buildErrResponse(reqs *pluginapi.AllocateRequest, podReqGPU uint) *pluginapi.AllocateResponse {
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		response := pluginapi.ContainerAllocateResponse{
			Envs: map[string]string{
				envNVGPU:               fmt.Sprintf("no-gpu-has-%d%s-to-run", podReqGPU, metric),
				EnvResourceIndex:       fmt.Sprintf("-1"),
				EnvResourceByPod:       fmt.Sprintf("%d", podReqGPU),
				EnvResourceByContainer: fmt.Sprintf("%d", uint(len(req.DevicesIDs))),
				EnvResourceByDev:       fmt.Sprintf("%d", getGPUMemory()),
			},
		}
		responses.ContainerResponses = append(responses.ContainerResponses, &response)
	}
	return &responses
}

// Allocate which return list of devices.
func (m *NvidiaDevicePlugin) Allocate(ctx context.Context,
	reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	responses := pluginapi.AllocateResponse{}

	log.Infoln("----Allocating GPU for gpu mem is started----")
	var (
		podReqGPU uint
		found     bool
		assumePod *v1.Pod
	)

	// podReqGPU = uint(0)
	for _, req := range reqs.ContainerRequests {
		podReqGPU += uint(len(req.DevicesIDs))
	}
	log.Infof("RequestPodGPUs: %d", podReqGPU)

	m.Lock()
	defer m.Unlock()
	log.Infoln("checking...")
	pods, err := getCandidatePods(m.queryKubelet, m.kubeletClient)
	if err != nil {
		log.Infof("invalid allocation requst: Failed to find candidate pods due to %v", err)
		return buildErrResponse(reqs, podReqGPU), nil
	}

	if log.V(4) {
		for _, pod := range pods {
			log.Infof("Pod %s in ns %s request GPU Memory %d with timestamp %v",
				pod.Name,
				pod.Namespace,
				getGPUMemoryFromPodResource(pod),
				getAssumeTimeFromPodAnnotation(pod))
		}
	}

	for _, pod := range pods {
		if getGPUMemoryFromPodResource(pod) == podReqGPU {
			log.Infof("Found Assumed GPU shared Pod %s in ns %s with GPU Memory %d",
				pod.Name,
				pod.Namespace,
				podReqGPU)
			assumePod = pod
			found = true
			break
		}
	}

	if found {
		id := getGPUIDFromPodAnnotation(assumePod)
		if id < 0 {
			log.Warningf("Failed to get the dev ", assumePod)
		}

		candidateDevID := ""
		if id >= 0 {
			ok := false
			candidateDevID, ok = m.GetDeviceNameByIndex(uint(id))
			if !ok {
				log.Warningf("Failed to find the dev for pod %v because it's not able to find dev with index %d",
					assumePod,
					id)
				id = -1
			}
		}

		if id < 0 {
			return buildErrResponse(reqs, podReqGPU), nil
		}
		log.Infof("gpu index %v,uuid: %v", id, candidateDevID)
		// 1. Create container requests
		for _, req := range reqs.ContainerRequests {
			reqGPU := uint(len(req.DevicesIDs))
			response := pluginapi.ContainerAllocateResponse{
				Envs: map[string]string{
					envNVGPU:               fmt.Sprintf("%v", id),
					EnvResourceIndex:       fmt.Sprintf("%d", id),
					EnvResourceByPod:       fmt.Sprintf("%d", podReqGPU),
					EnvResourceByContainer: fmt.Sprintf("%d", reqGPU),
					EnvResourceByDev:       fmt.Sprintf("%d", getGPUMemory()),
				},
			}
			if m.disableCGPUIsolation {
				response.Envs["CGPU_DISABLE"] = "true"
			}
			responses.ContainerResponses = append(responses.ContainerResponses, &response)
		}

		// 2. Update Pod spec
		patchedAnnotationBytes, err := patchPodAnnotationSpecAssigned()
		if err != nil {
			return buildErrResponse(reqs, podReqGPU), nil
		}
		_, err = clientset.CoreV1().Pods(assumePod.Namespace).Patch(assumePod.Name, types.StrategicMergePatchType, patchedAnnotationBytes)
		if err != nil {
			// the object has been modified; please apply your changes to the latest version and try again
			if err.Error() == OptimisticLockErrorMsg {
				// retry
				_, err = clientset.CoreV1().Pods(assumePod.Namespace).Patch(assumePod.Name, types.StrategicMergePatchType, patchedAnnotationBytes)
				if err != nil {
					log.Warningf("Failed due to %v", err)
					return buildErrResponse(reqs, podReqGPU), nil
				}
			} else {
				log.Warningf("Failed due to %v", err)
				return buildErrResponse(reqs, podReqGPU), nil
			}
		}

	} else if len(m.devNameMap) == 1 {
		var devName string
		var devIndex uint
		for d, index := range m.devNameMap {
			devName = d
			devIndex = index
			break
		}
		log.Infof("this node has only one gpu device,skip to search pod and directly specify the device  %v(%v) for container", devIndex, devName)
		for _, req := range reqs.ContainerRequests {
			reqGPU := uint(len(req.DevicesIDs))
			response := pluginapi.ContainerAllocateResponse{
				Envs: map[string]string{
					envNVGPU:               devName,
					EnvResourceIndex:       fmt.Sprintf("%d", devIndex),
					EnvResourceByPod:       fmt.Sprintf("%d", podReqGPU),
					EnvResourceByContainer: fmt.Sprintf("%d", reqGPU),
					EnvResourceByDev:       fmt.Sprintf("%d", getGPUMemory()),
				},
			}
			if m.disableCGPUIsolation {
				response.Envs["CGPU_DISABLE"] = "true"
			}
			responses.ContainerResponses = append(responses.ContainerResponses, &response)
		}
		log.Infof("get allocated GPUs info %v", responses)
		return &responses, nil

	} else {
		log.Warningf("invalid allocation requst: request GPU memory %d can't be satisfied.",
			podReqGPU)
		// return &responses, fmt.Errorf("invalid allocation requst: request GPU memory %d can't be satisfied", reqGPU)
		return buildErrResponse(reqs, podReqGPU), nil
	}

	podName := ""
	if assumePod != nil {
		podName = assumePod.Name
	}
	log.Infof("pod %v, new allocated GPUs info %v", podName, &responses)
	log.Infof("----Allocating GPU for gpu mem for %v is ended----", podName)
	// // Add this to make sure the container is created at least
	// currentTime := time.Now()

	// currentTime.Sub(lastAllocateTime)

	return &responses, nil
}
