# ── build ────────────────────────────────────────────────────────────────
# 版本与开发机（node v24）对齐，避免工具链漂移。
#
# 另有一条踩过的坑值得留在这里：package-lock.json 必须在 **linux** 上生成。
# 它原先是在 macOS 上生成的，npm 只记录了 darwin 侧实际会走到的依赖，漏掉了
# @emnapi/core 与 @emnapi/runtime —— 那是某个原生包在 linux 上回退到 wasm
# 实现时才需要的两条传递依赖。结果是本地一切正常（大家用宽松的 npm install），
# 而任何 linux 上的 npm ci 都直接 EUSAGE «lock file out of sync»，
# 镜像构建和 CI 会一起挂。已用 `docker run --rm -v "$PWD:/w" -w /w \
# node:24-alpine npm install --package-lock-only` 重新生成。
# 换 node 版本或加原生依赖后，锁文件要照这个方式重新生成，不能在 mac 上跑。
FROM node:24-alpine AS build

WORKDIR /src

# 先只拷贝锁文件跑 npm ci，让依赖层能被缓存——改业务代码不必重装 node_modules。
COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# npm run build 是 `tsc -b && vite build`：类型检查和打包一起做。
# 不要图快换成只跑 vite build —— vite 用 esbuild 转译，**完全不做类型检查**，
# 类型错误会一路溜进产物里。
RUN npm run build

# ── runtime ──────────────────────────────────────────────────────────────
FROM nginx:1.27-alpine

COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
