package main

import (
	"flag"

	"github.com/AliyunContainerService/gpushare-device-plugin/pkg/gpu/nvidia"
	log "github.com/golang/glog"
)

var (
	mps         = flag.Bool("mps", false, "Enable or Disable MPS")
	healthCheck = flag.Bool("health-check", false, "Enable or disable Health check")
	metric      = flag.String("metric", "Gi", "Set metric of the GPU Memroy, support 'Gi' and 'Mi'")
)

func main() {
	flag.Parse()
	log.V(1).Infoln("Start gpushare device plugin")
	ngm := nvidia.NewSharedGPUManager(*mps, *healthCheck, translateMetrics(*metric))
	err := ngm.Run()
	if err != nil {
		log.Fatalf("Failed due to %v", err)
	}
}

func translateMetrics(value string) nvidia.BinaryPrefix {
	metric := nvidia.BinaryPrefix(value)
	switch metric {
	case nvidia.MiBPrefix:
	case nvidia.GiBPrefix:
	default:
		log.Warningf("Unsupported metric: %s, use metric Gi as default", value)
		metric = nvidia.GiBinaryPrefix
	}

	return metric
}
