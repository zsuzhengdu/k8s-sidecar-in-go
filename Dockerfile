FROM golang:1.15 as builder

WORKDIR /go/src/github.com/zsuzhengdu/k8s-sidercar
COPY . /go/src/github.com/zsuzhengdu/k8s-sidercar

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /go/bin/k8s-sidercar /go/src/github.com/zsuzhengdu/k8s-sidercar/main.go

FROM alpine:3
RUN apk --update add ca-certificates
RUN addgroup -S k8s-sidercar && adduser -S -G k8s-sidercar k8s-sidercar
USER k8s-sidercar
COPY --from=builder /go/bin/k8s-sidercar /usr/local/bin/k8s-sidercar

ENTRYPOINT ["k8s-sidercar"]
