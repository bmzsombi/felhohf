podman build -t detector -f deploy/Dockerfile .
podman tag bmzsombi/detector localhost/detector
podman push bzsombi/detector:latest