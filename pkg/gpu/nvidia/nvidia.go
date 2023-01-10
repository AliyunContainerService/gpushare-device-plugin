package nvidia

import (
	"fmt"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	log "github.com/golang/glog"

	"golang.org/x/net/context"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	metric MemoryUnit
)

func check(ret nvml.Return) {
	if ret != nvml.SUCCESS {
		log.Fatalln("Fatal: ", ret)
	}
}

func generateFakeDeviceID(realID string, fakeCounter uint64) *string {
	fakeId := fmt.Sprintf("%s-_-%d", realID, fakeCounter)
	return &fakeId
}

func extractRealDeviceID(fakeDeviceID string) string {
	return strings.Split(fakeDeviceID, "-_-")[0]
}

func getDeviceCount() int {
	n, ret := nvml.DeviceGetCount()
	check(ret)
	return n
}

func getDevicePaths(d *nvml.Device) ([]string, error) {
	minor, ret := d.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device minor number: %v", ret)
	}
	path := fmt.Sprintf("/dev/nvidia%d", minor)

	return []string{path}, nil
}

func getDevices() ([]*pluginapi.Device, map[string]uint) {
	n, ret := nvml.DeviceGetCount()
	check(ret)

	var devs []*pluginapi.Device
	realDevNames := map[string]uint{}
	for i := 0; i < n; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		check(ret)
		// realDevNames = append(realDevNames, d.UUID)
		var id uint
		uuid, ret := d.GetUUID()
		check(ret)
		memory, ret := d.GetMemoryInfo()
		check(ret)
		paths, err := getDevicePaths(&d)
		if err != nil {
			continue
		}
		log.Infof("Deivce %s, path %v, memory %+v", uuid, paths, memory)
		realDevNames[uuid] = id
		memoryGiB := memory.Free / 1024 / 1024 / 1024
		log.V(1).Infof("gpu memory %d G \n", memoryGiB)
		for j := uint64(0); j < memoryGiB; j++ {
			fakeID := generateFakeDeviceID(uuid, j)
			log.Infof("# Add %d device ID: %s\n", j, *fakeID)

			devs = append(devs, &pluginapi.Device{
				ID:     *fakeID,
				Health: pluginapi.Healthy,
			})
		}
	}

	return devs, realDevNames
}

func deviceExists(devs []*pluginapi.Device, id string) bool {
	for _, d := range devs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func watchXIDs(ctx context.Context, devs []*pluginapi.Device, xids chan<- *pluginapi.Device) {
	eventSet, ret := nvml.EventSetCreate()
	check(ret)
	defer nvml.EventSetFree(eventSet)

	for _, d := range devs {
		realDeviceID := extractRealDeviceID(d.ID)
		device, ret := nvml.DeviceGetHandleByUUID(realDeviceID)
		if ret != nvml.SUCCESS {
			continue
		}
		ret = nvml.DeviceRegisterEvents(device, nvml.EventTypeXidCriticalError, eventSet)
		if ret != nvml.SUCCESS {
			log.Infof("Warning: %s (%s) is too old to support healthchecking: %d. Marking it unhealthy.", realDeviceID, d.ID, ret)

			xids <- d
			continue
		}
		check(ret)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e, ret := nvml.EventSetWait(eventSet, 5000)
		if ret != nvml.EventTypeXidCriticalError {
			continue
		}

		// FIXME: formalize the full list and document it.
		// http://docs.nvidia.com/deploy/xid-errors/index.html#topic_4
		// Application errors: the GPU should still be healthy
		if e.EventData == 31 || e.EventData == 43 || e.EventData == 45 {
			continue
		}

		uuid, ret := e.Device.GetUUID()
		if ret != nvml.SUCCESS || len(uuid) == 0 {
			// All devices are unhealthy
			for _, d := range devs {
				xids <- d
			}
			continue
		}

		for _, d := range devs {
			if extractRealDeviceID(d.ID) == uuid {
				xids <- d
			}
		}
	}
}
