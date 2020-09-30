FROM golang:1.10-stretch as build

WORKDIR /go/src/github.com/AliyunContainerService/gpushare-device-plugin
COPY . .

RUN export CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files' && \
    go build -ldflags="-s -w" -o /go/bin/gpushare-device-plugin-v2 cmd/nvidia/main.go

RUN go get github.com/prometheus/client_golang/prometheus && \
    go get github.com/prometheus/client_golang/prometheus/promhttp 

RUN go build -o /go/bin/kubectl-inspect-gpushare-v2 cmd/inspect/*.go

FROM debian:bullseye-slim

ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

COPY --from=build /go/bin/gpushare-device-plugin-v2 /usr/bin/gpushare-device-plugin-v2

COPY --from=build /go/bin/kubectl-inspect-gpushare-v2 /usr/bin/kubectl-inspect-gpushare-v2

CMD ["gpushare-device-plugin-v2","-logtostderr"]
