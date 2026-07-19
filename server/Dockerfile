# ── build ────────────────────────────────────────────────────────────────
FROM golang:1.25 AS build

WORKDIR /src

# 先只拷贝依赖清单，让 go mod download 这一层能被缓存——改业务代码不必
# 重新拉整棵依赖树。
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev

# CGO_ENABLED=0 换取一个静态二进制，才能跑在下面那个没有 libc 的 distroless 上。
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/server ./cmd/server

# 迁移不是应用自己跑的（见 Makefile 的 migrate-* 目标），部署时需要一个独立
# 的迁移步骤。把 migrate CLI 一并编进镜像，这样这一个镜像就能同时充当
# 「应用」和「迁移器」两个角色，docker-compose 不必再拉第三方镜像、也不必
# 依赖宿主机上有这个仓库的 migrations 目录。
RUN CGO_ENABLED=0 GOOS=linux go install -tags 'postgres' \
      github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1

# ── runtime ──────────────────────────────────────────────────────────────
# distroless 而不是 alpine：镜像里没有 shell、没有包管理器，攻击面小得多。
# 代价是进不去容器 debug，需要时用 :debug 变体临时换一下。
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/server      /app/server
COPY --from=build /go/bin/migrate  /app/migrate
COPY --from=build /src/migrations  /app/migrations

# 配置是**纯 YAML、没有任何环境变量兜底**（这是刻意的设计，见
# internal/config/config.go）。所以镜像里不放 config.yaml，由部署方挂载进来；
# 缺文件时进程会直接启动失败并说清楚缺什么，而不是带着一半默认值悄悄跑起来。
USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/app/server"]
CMD ["--config", "/app/config.yaml"]
