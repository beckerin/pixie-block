FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /autoscaler .

FROM docker:27.5.1-cli

COPY --from=build /autoscaler /usr/local/bin/autoscaler
WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/autoscaler"]
