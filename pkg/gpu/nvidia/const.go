package nvidia

import (
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

// MemoryUnit describes GPU Memory, now only supports Gi, Mi
type MemoryUnit string

const (
	resourceName  = "aliyun.com/gpu-mem"
	resourceCount = "aliyun.com/gpu-count"
	serverSock    = pluginapi.DevicePluginPath + "aliyungpushare.sock"

	OptimisticLockErrorMsg = "the object has been modified; please apply your changes to the latest version and try again"

	allHealthChecks             = "xids"
	containerTypeLabelKey       = "io.kubernetes.docker.type"
	containerTypeLabelSandbox   = "podsandbox"
	containerTypeLabelContainer = "container"
	containerLogPathLabelKey    = "io.kubernetes.container.logpath"
	sandboxIDLabelKey           = "io.kubernetes.sandbox.id"

	envNVGPU                     = "NVIDIA_VISIBLE_DEVICES"
	EnvResourceIndex             = "ALIYUN_COM_GPU_MEM_IDX"
	EnvResourceByPod             = "ALIYUN_COM_GPU_MEM_POD"
	EnvResourceByContainer       = "ALIYUN_COM_GPU_MEM_CONTAINER"
	EnvResourceByDev             = "ALIYUN_COM_GPU_MEM_DEV"
	EnvAssignedFlag              = "ALIYUN_COM_GPU_MEM_ASSIGNED"
	EnvResourceAssumeTime        = "ALIYUN_COM_GPU_MEM_ASSUME_TIME"
	EnvMPSPipeDirectory          = "CUDA_MPS_PIPE_DIRECTORY"
	EnvMPSActiveThreadPercentage = "CUDA_MPS_ACTIVE_THREAD_PERCENTAGE"
	EnvResourceAssignTime        = "ALIYUN_COM_GPU_MEM_ASSIGN_TIME"

	GiBPrefix = MemoryUnit("GiB")
	MiBPrefix = MemoryUnit("MiB")
)
