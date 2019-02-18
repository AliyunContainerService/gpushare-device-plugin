package main

import (
	"flag"

	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/gpu/nvidia"
	log "github.com/golang/glog"
)

var (
	mps         = flag.Bool("mps", false, "Enable or Disable MPS")
	healthCheck = flag.Bool("health-check", false, "Enable or disable Health check")
)

func main() {
	flag.Parse()
	log.V(1).Infoln("Start gpushare device plugin")

	ngm := nvidia.NewSharedGPUManager(*mps, *healthCheck)
	err := ngm.Run()
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}
}
