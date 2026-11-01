FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./... && CGO_ENABLED=0 go build -o /out/ ./cmd/...

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/ /app/
EXPOSE 8765
CMD ["/app/nro-server"]
