package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/gpu/nvidia"
	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/kubelet/client"
	log "github.com/golang/glog"
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
	"net/http"
	"os"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"time"
)

var (
	mps              = flag.Bool("mps", false, "Enable or Disable MPS")
	healthCheck      = flag.Bool("health-check", false, "Enable or disable Health check")
	memoryUnit       = flag.String("memory-unit", "GiB", "Set memoryUnit of the GPU Memroy, support 'GiB' and 'MiB'")
	queryFromKubelet = flag.Bool("query-kubelet", false, "Query pending pods from kubelet instead of kube-apiserver")
	kubeletAddress   = flag.String("kubelet-address", "0.0.0.0", "Kubelet IP Address")
	kubeletPort      = flag.Uint("kubelet-port", 10250, "Kubelet listened Port")
	clientCert       = flag.String("client-cert", "", "Kubelet TLS client certificate")
	clientKey        = flag.String("client-key", "", "Kubelet TLS client key")
	token            = flag.String("token", "", "Kubelet client bearer token")
	timeout          = flag.Int("timeout", 10, "Kubelet client http timeout duration")
)

func buildKubeletClient() *client.KubeletClient {
	if *clientCert == "" && *clientKey == "" && *token == "" {
		tokenByte, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			panic(fmt.Errorf("in cluster mode, find token failed, error: %v", err))
		}
		tokenStr := string(tokenByte)
		token = &tokenStr
	}
	kubeletClient, err := client.NewKubeletClient(&client.KubeletClientConfig{
		Address: *kubeletAddress,
		Port:    *kubeletPort,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure:   true,
			ServerName: "gpushare-device-plugin",
			CertFile:   *clientCert,
			KeyFile:    *clientKey,
		},
		BearerToken: *token,
		HTTPTimeout: time.Duration(*timeout) * time.Second,
	})
	if err != nil {
		panic(err)
	}
	return kubeletClient
}

func main() {
	flag.Parse()
	log.V(1).Infoln("Start gpushare device plugin")
	ctx := context.Background()
	kubeletClient := buildKubeletClient()
	ngm := nvidia.NewSharedGPUManager(*mps, *healthCheck, *queryFromKubelet, translatememoryUnits(*memoryUnit), kubeletClient)
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("Recover error %+v \n", err)
		}
		os.Exit(1)
	}()
	r := gin.New()
	_ = r.SetTrustedProxies(nil)

	// pprof
	pprof.Register(r)
	httpSrv := &http.Server{Addr: ":8085", Handler: r}
	go func() {
		err := httpSrv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			log.Infof("HTTP server closed")
		} else {
			log.Warningf("Error occurs in http server: %v", err)
		}
	}()
	err := ngm.Run(ctx)
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}
}

func translatememoryUnits(value string) nvidia.MemoryUnit {
	memoryUnit := nvidia.MemoryUnit(value)
	switch memoryUnit {
	case nvidia.MiBPrefix:
	case nvidia.GiBPrefix:
	default:
		log.Warningf("Unsupported memory unit: %s, use memoryUnit Gi as default", value)
		memoryUnit = nvidia.GiBPrefix
	}

	return memoryUnit
}
