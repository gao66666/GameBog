FROM golang:1.24-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /app/goblog .

# 运行阶段
FROM alpine:3.19

WORKDIR /app

# 👇 关键：替换为阿里云 Alpine 软件源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 现在安装软件包就会走阿里云镜像了
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/goblog .
COPY --from=builder /app/setting ./setting
COPY --from=builder /app/web/templates ./web/templates
COPY --from=builder /app/web/static ./web/static

ENV TZ=Asia/Shanghai

EXPOSE 8084

CMD ["./goblog"]