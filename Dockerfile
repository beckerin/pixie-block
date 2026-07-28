FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pixie ./cmd/server && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/genkeys ./tools/genkeys.go

FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=build /out/pixie /out/genkeys ./
COPY dist/ dist/
COPY docs/ docs/
COPY config/taxes.json config/
COPY tools/accounts.json tools/
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 80 90

ENTRYPOINT ["/entrypoint.sh"]
