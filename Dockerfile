FROM golang:1.15-alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-service-mirror
COPY . /go/src/github.com/utilitywarehouse/kube-service-mirror
ENV CGO_ENABLED 0
RUN go test ./...
RUN go build -o /kube-service-mirror .

FROM alpine:3.12
COPY --from=build /kube-service-mirror /kube-service-mirror
CMD [ "/kube-service-mirror" ]
