FROM golang:latest AS build

WORKDIR /app

COPY src/go.mod ./
COPY src/go.sum ./

RUN go mod download
RUN go mod verify

# RUN go mod tidy

COPY src/ ./
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o /app/helloworld

FROM scratch
COPY --from=build /app/helloworld .
COPY static /static
CMD ["/helloworld"]