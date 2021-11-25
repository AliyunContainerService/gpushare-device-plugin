package nvidia

import (
	"fmt"
	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/kubelet/client"
	"syscall"
	"os"
	"time"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"github.com/fsnotify/fsnotify"
	log "github.com/golang/glog"
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

type sharedGPUManager struct {
	enableMPS     bool
	healthCheck   bool
	queryKubelet  bool
	kubeletClient *client.KubeletClient
}

func NewSharedGPUManager(enableMPS, healthCheck, queryKubelet bool, bp MemoryUnit, client *client.KubeletClient) *sharedGPUManager {
	metric = bp
	return &sharedGPUManager{
		enableMPS:     enableMPS,
		healthCheck:   healthCheck,
		queryKubelet:  queryKubelet,
		kubeletClient: client,
	}
}

func (ngm *sharedGPUManager) Run() error {
	log.V(1).Infoln("Loading NVML")

	if err := nvml.Init(); err != nil {
		log.V(1).Infof("Failed to initialize NVML: %s.", err)
		log.V(1).Infof("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
		select {}
	}
	defer func() { log.V(1).Infoln("Shutdown of NVML returned:", nvml.Shutdown()) }()

	log.V(1).Infoln("Fetching devices.")
	if getDeviceCount() == uint(0) {
		log.V(1).Infoln("No devices found. Waiting indefinitely.")
		select {}
	}

	log.V(1).Infoln("Starting FS watcher.")
	watcher, err := newFSWatcher(pluginapi.DevicePluginPath)
	if err != nil {
		log.V(1).Infoln("Failed to created FS watcher.")
		return err
	}
	defer watcher.Close()

	log.V(1).Infoln("Starting OS watcher.")
	sigs := newOSWatcher(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	restart := true
	var devicePlugin *NvidiaDevicePlugin

L:
	for {
		if restart {
			if devicePlugin != nil {
				devicePlugin.Stop()
			}

			devicePlugin, err = NewNvidiaDevicePlugin(ngm.enableMPS, ngm.healthCheck, ngm.queryKubelet, ngm.kubeletClient)
			if err != nil {
				log.Warningf("Failed to get device plugin due to %v", err)
				os.Exit(1)
			} else if err = devicePlugin.Serve(); err != nil {
				log.Warningf("Failed to start device plugin due to %v", err)
				os.Exit(2)
			} else {
				restart = false
			}
		}

		select {
		case event := <-watcher.Events:
			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				log.V(1).Infof("inotify: %s created, restarting.", pluginapi.KubeletSocket)
				restart = true
			}

		case err := <-watcher.Errors:
			log.Warningf("inotify: %s", err)

		case s := <-sigs:
			switch s {
			case syscall.SIGHUP:
				log.V(1).Infoln("Received SIGHUP, restarting.")
				restart = true
			case syscall.SIGQUIT:
				t := time.Now()
				timestamp := fmt.Sprint(t.Format("20060102150405"))
				log.Infoln("generate core dump")
				coredump("/etc/kubernetes/go_" + timestamp + ".txt")
			default:
				log.V(1).Infof("Received signal \"%v\", shutting down.", s)
				devicePlugin.Stop()
				break L
			}
		}
	}

	return nil
}
