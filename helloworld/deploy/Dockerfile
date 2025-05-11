FROM golang:latest AS build

COPY src/ /
WORKDIR /

# RUN go mod tidy
RUN go mod download

RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o helloworld

FROM scratch
COPY --from=build /helloworld .
COPY static /static
CMD ["/helloworld"]