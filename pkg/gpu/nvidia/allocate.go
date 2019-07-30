package nvidia

import (
	"fmt"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/net/context"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (m *NvidiaDevicePlugin) buildErrResponse(reqs *pluginapi.AllocateRequest, podReqGPU uint) *pluginapi.AllocateResponse {
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		response := pluginapi.ContainerAllocateResponse{
			Envs: map[string]string{
				envNVGPU:               fmt.Sprintf("no-gpu-has-%dGiB-to-run", podReqGPU),
				EnvResourceIndex:       fmt.Sprintf("-1"),
				EnvResourceByPod:       fmt.Sprintf("%d", podReqGPU),
				EnvResourceByContainer: fmt.Sprintf("%d", uint(len(req.DevicesIDs))),
				EnvResourceByDev:       fmt.Sprintf("%d", getGPUMemory()),
			},
		}
		if m.mps {
			response.Envs[EnvMPSActiveThreadPercentage] = fmt.Sprintf("%d", 100*uint(len(req.DevicesIDs))/getGPUMemory())
			response.Envs[EnvMPSPipeDirectory] = fmt.Sprintf(m.mpspipe)
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
	pods, err := getCandidatePods()
	if err != nil {
		log.Infof("invalid allocation requst: Failed to find candidate pods due to %v", err)
		return m.buildErrResponse(reqs, podReqGPU), nil
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
			return m.buildErrResponse(reqs, podReqGPU), nil
		}

		// 1. Create container requests
		for _, req := range reqs.ContainerRequests {
			reqGPU := uint(len(req.DevicesIDs))
			response := pluginapi.ContainerAllocateResponse{
				Envs: map[string]string{
					envNVGPU:               candidateDevID,
					EnvResourceIndex:       fmt.Sprintf("%d", id),
					EnvResourceByPod:       fmt.Sprintf("%d", podReqGPU),
					EnvResourceByContainer: fmt.Sprintf("%d", reqGPU),
					EnvResourceByDev:       fmt.Sprintf("%d", getGPUMemory()),
				},
			}
			if m.mps {
				response.Envs[EnvMPSActiveThreadPercentage] = fmt.Sprintf("%d", 100*reqGPU/getGPUMemory())
				response.Envs[EnvMPSPipeDirectory] = fmt.Sprintf(m.mpspipe)
				mount := pluginapi.Mount{
					ContainerPath: m.mpspipe,
					HostPath:      m.mpspipe,
				}
				response.Mounts = append(response.Mounts, &mount)
			}
			responses.ContainerResponses = append(responses.ContainerResponses, &response)
		}

		// 2. Update Pod spec
		newPod := updatePodAnnotations(assumePod)
		_, err = clientset.CoreV1().Pods(newPod.Namespace).Update(newPod)
		if err != nil {
			// the object has been modified; please apply your changes to the latest version and try again
			if err.Error() == OptimisticLockErrorMsg {
				// retry
				pod, err := clientset.CoreV1().Pods(assumePod.Namespace).Get(assumePod.Name, metav1.GetOptions{})
				if err != nil {
					log.Warningf("Failed due to %v", err)
					return m.buildErrResponse(reqs, podReqGPU), nil
				}
				newPod = updatePodAnnotations(pod)
				_, err = clientset.CoreV1().Pods(newPod.Namespace).Update(newPod)
				if err != nil {
					log.Warningf("Failed due to %v", err)
					return m.buildErrResponse(reqs, podReqGPU), nil
				}
			} else {
				log.Warningf("Failed due to %v", err)
				return m.buildErrResponse(reqs, podReqGPU), nil
			}
		}

	} else {
		log.Warningf("invalid allocation requst: request GPU memory %d can't be satisfied.",
			podReqGPU)
		// return &responses, fmt.Errorf("invalid allocation requst: request GPU memory %d can't be satisfied", reqGPU)
		return m.buildErrResponse(reqs, podReqGPU), nil
	}

	log.Infof("new allocated GPUs info %v", &responses)
	log.Infoln("----Allocating GPU for gpu mem is ended----")
	// // Add this to make sure the container is created at least
	// currentTime := time.Now()

	// currentTime.Sub(lastAllocateTime)

	return &responses, nil
}
