package nvidia

import (
	"fmt"
	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/kubelet/client"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/fsnotify/fsnotify"
	log "github.com/golang/glog"
	"golang.org/x/net/context"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"os"
	"syscall"
	"time"
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

func (ngm *sharedGPUManager) Run(ctx context.Context) error {
	log.V(1).Infoln("Loading NVML")

	if ret := nvml.Init(); ret != nvml.SUCCESS {
		log.V(1).Infof("Failed to initialize NVML: %d.", ret)
		log.V(1).Infof("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
		select {}
	}
	defer func() { log.V(1).Infoln("Shutdown of NVML returned:", nvml.Shutdown()) }()

	log.V(1).Infoln("Fetching devices.")
	if getDeviceCount() == 0 {
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
			log.V(1).Infof("try new nvidia device plugin")
			devicePlugin, err = NewNvidiaDevicePlugin(ctx, ngm.enableMPS, ngm.healthCheck, ngm.queryKubelet, ngm.kubeletClient)
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
		log.V(1).Infoln("run into for select")

		select {
		case event := <-watcher.Events:
			log.V(1).Infof("receive event into for %v\n", event)

			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				log.V(1).Infof("inotify: %s created, restarting.", pluginapi.KubeletSocket)
				restart = true
			}

		case err := <-watcher.Errors:
			log.Warningf("inotify: %s", err)

		case s := <-sigs:
			log.V(1).Infoln("receive sigs into for", s)

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
