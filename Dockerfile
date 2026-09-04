# 云端编译阶段
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o mxgt ./cmd/server

# 运行阶段（单文件 + 数据目录）
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/mxgt .
EXPOSE 8080
CMD ["./mxgt"]
