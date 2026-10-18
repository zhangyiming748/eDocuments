# ---- 构建阶段 ----
FROM golang:1.27-bookworm AS build
WORKDIR /src

# 先拉依赖以利用构建缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 依赖均为纯 Go（modernc 风格），CGO_ENABLED=0 静态编译
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /edocs ./cmd/edocs

# ---- 运行阶段：Go 二进制 + 系统 tesseract（中文语言包随镜像打包，纯离线可用）----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        tesseract-ocr \
        tesseract-ocr-chi-sim \
        tesseract-ocr-eng \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /edocs /usr/local/bin/edocs

# 扫描件与缓存的默认挂载点
VOLUME ["/data/scan", "/data/cache"]

ENTRYPOINT ["edocs"]
# 默认启动搜索服务；ES/扫描根目录/缓存路径由 compose 环境变量注入。
# 索引导入执行：docker compose run --rm edocs index
CMD ["serve", "--addr", ":8080"]
