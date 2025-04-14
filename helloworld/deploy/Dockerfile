FROM golang:latest AS build

COPY src/ /
WORKDIR /

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o helloworld

FROM scratch
COPY --from=build /helloworld .
COPY static /static
CMD ["/helloworld"]