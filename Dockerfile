# ===== 阶段 1：构建 Vue 前端 =====
FROM node:22-alpine AS webapp-builder

WORKDIR /webapp

ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com

COPY webapp/package.json ./
RUN npm install

COPY webapp/ .
RUN npm run build

# ===== 阶段 2：构建 Go =====
FROM golang:1.24-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=webapp-builder /webapp/dist ./webapp/dist

RUN go build -o /app/goblog .

# ===== 阶段 3：运行 =====
FROM alpine:3.19

WORKDIR /app

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/goblog .
COPY --from=builder /app/setting ./setting
COPY --from=builder /app/web/templates ./web/templates
COPY --from=builder /app/web/static ./web/static
COPY --from=builder /app/webapp/dist ./webapp/dist

ENV TZ=Asia/Shanghai

EXPOSE 8084

CMD ["./goblog"]
