package nvidia

import (
	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/kubelet/client"
	"net"
	"os"
	"path"
	"sync"
	"time"

	log "github.com/golang/glog"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

// NvidiaDevicePlugin implements the Kubernetes device plugin API
type NvidiaDevicePlugin struct {
	devs                 []*pluginapi.Device
	realDevNames         []string
	devNameMap           map[string]uint
	devIndxMap           map[uint]string
	socket               string
	mps                  bool
	healthCheck          bool
	disableCGPUIsolation bool
	stop                 chan struct{}
	health               chan *pluginapi.Device
	queryKubelet         bool
	kubeletClient        *client.KubeletClient

	server *grpc.Server
	sync.RWMutex
}

// NewNvidiaDevicePlugin returns an initialized NvidiaDevicePlugin
func NewNvidiaDevicePlugin(mps, healthCheck, queryKubelet bool, client *client.KubeletClient) (*NvidiaDevicePlugin, error) {
	devs, devNameMap := getDevices()
	devList := []string{}

	for dev, _ := range devNameMap {
		devList = append(devList, dev)
	}

	log.Infof("Device Map: %v", devNameMap)
	log.Infof("Device List: %v", devList)

	err := patchGPUCount(len(devList))
	if err != nil {
		return nil, err
	}
	disableCGPUIsolation, err := disableCGPUIsolationOrNot()
	if err != nil {
		return nil, err
	}
	return &NvidiaDevicePlugin{
		devs:                 devs,
		realDevNames:         devList,
		devNameMap:           devNameMap,
		socket:               serverSock,
		mps:                  mps,
		healthCheck:          healthCheck,
		disableCGPUIsolation: disableCGPUIsolation,
		stop:                 make(chan struct{}),
		health:               make(chan *pluginapi.Device),
		queryKubelet:         queryKubelet,
		kubeletClient:        client,
	}, nil
}

func (m *NvidiaDevicePlugin) GetDeviceNameByIndex(index uint) (name string, found bool) {
	if len(m.devIndxMap) == 0 {
		m.devIndxMap = map[uint]string{}
		for k, v := range m.devNameMap {
			m.devIndxMap[v] = k
		}
		log.Infof("Get devIndexMap: %v", m.devIndxMap)
	}

	name, found = m.devIndxMap[index]
	return name, found
}

func (m *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// dial establishes the gRPC communication with the registered device plugin.
func dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, err
	}

	return c, nil
}

// Start starts the gRPC server of the device plugin
func (m *NvidiaDevicePlugin) Start() error {
	err := m.cleanup()
	if err != nil {
		return err
	}

	sock, err := net.Listen("unix", m.socket)
	if err != nil {
		return err
	}

	m.server = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(m.server, m)

	go m.server.Serve(sock)

	// Wait for server to start by launching a blocking connexion
	conn, err := dial(m.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	go m.healthcheck()

	lastAllocateTime = time.Now()

	return nil
}

// Stop stops the gRPC server
func (m *NvidiaDevicePlugin) Stop() error {
	if m.server == nil {
		return nil
	}

	m.server.Stop()
	m.server = nil
	close(m.stop)

	return m.cleanup()
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (m *NvidiaDevicePlugin) Register(kubeletEndpoint, resourceName string) error {
	conn, err := dial(kubeletEndpoint, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(m.socket),
		ResourceName: resourceName,
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// ListAndWatch lists devices and update that list according to the health status
func (m *NvidiaDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})

	for {
		select {
		case <-m.stop:
			return nil
		case d := <-m.health:
			// FIXME: there is no way to recover from the Unhealthy state.
			d.Health = pluginapi.Unhealthy
			s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})
		}
	}
}

func (m *NvidiaDevicePlugin) unhealthy(dev *pluginapi.Device) {
	m.health <- dev
}

func (m *NvidiaDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (m *NvidiaDevicePlugin) cleanup() error {
	if err := os.Remove(m.socket); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (m *NvidiaDevicePlugin) healthcheck() {
	ctx, cancel := context.WithCancel(context.Background())

	var xids chan *pluginapi.Device
	if m.healthCheck {
		xids = make(chan *pluginapi.Device)
		go watchXIDs(ctx, m.devs, xids)
	}

	for {
		select {
		case <-m.stop:
			cancel()
			return
		case dev := <-xids:
			m.unhealthy(dev)
		}
	}
}

// Serve starts the gRPC server and register the device plugin to Kubelet
func (m *NvidiaDevicePlugin) Serve() error {
	err := m.Start()
	if err != nil {
		log.Infof("Could not start device plugin: %s", err)
		return err
	}
	log.Infoln("Starting to serve on", m.socket)

	err = m.Register(pluginapi.KubeletSocket, resourceName)
	if err != nil {
		log.Infof("Could not register device plugin: %s", err)
		m.Stop()
		return err
	}
	log.Infoln("Registered device plugin with Kubelet")

	return nil
}
