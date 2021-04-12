FROM golang:1.15-alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/semaphore-service-mirror
COPY . /go/src/github.com/utilitywarehouse/semaphore-service-mirror
ENV CGO_ENABLED 0
RUN go test ./...
RUN go build -o /semaphore-service-mirror .

FROM alpine:3.12
COPY --from=build /semaphore-service-mirror /semaphore-service-mirror
CMD [ "/semaphore-service-mirror" ]
