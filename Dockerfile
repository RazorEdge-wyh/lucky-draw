# ---------------------------------------------------------------------------
# 多阶段构建：编译阶段用官方 Go 镜像，运行阶段只保留一个静态二进制。
# 最终镜像基于 distroless static，无 shell、无包管理器，攻击面极小。
# ---------------------------------------------------------------------------
FROM golang:1.22-alpine AS builder

WORKDIR /src

# 先只拷贝依赖描述文件，让依赖层可以被 Docker 缓存复用。
COPY go.mod ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 生成静态链接二进制，才能跑在 distroless/static 上。
# -trimpath 去掉本地路径，-s -w 去掉符号表和调试信息，显著减小体积。
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/lucky-draw .

# --- 运行阶段 ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/lucky-draw /lucky-draw

# nonroot 用户（uid 65532），不需要 root 权限。
USER nonroot:nonroot

ENV PORT=8080
EXPOSE 8080

# distroless 没有 shell 也没有 curl，所以健康检查由二进制自己发起 HTTP 请求。
# 注意必须用 CMD 的 exec 形式（JSON 数组），不能走 shell。
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/lucky-draw", "-healthcheck"]

# distroless 没有 shell，因此用 exec 形式；健康检查见上方 HEALTHCHECK。
ENTRYPOINT ["/lucky-draw"]
