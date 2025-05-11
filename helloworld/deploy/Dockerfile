FROM golang:latest AS build

COPY src/ /
WORKDIR /

COPY /etc/rancher/k3s/k3s.yaml /root/.kube/config

# RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o helloworld

FROM scratch
COPY --from=build /helloworld .
COPY static /static
CMD ["/helloworld"]