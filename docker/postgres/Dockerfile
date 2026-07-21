# 本地开发用 PostgreSQL（对齐 Step 3 DSN）
# 官方镜像足够；此 Dockerfile 只固定版本 + 可选初始化脚本目录
FROM postgres:16-alpine

# 默认与 docker-compose / DSN 一致（compose 里仍可覆盖）
ENV POSTGRES_USER=postgres \
    POSTGRES_PASSWORD=obs_dev \
    POSTGRES_DB=observability \
    TZ=Asia/Shanghai

# 把 *.sql / *.sh 放进 docker-entrypoint-initdb.d/ 会在首次启动时执行
# COPY ./init/ /docker-entrypoint-initdb.d/

EXPOSE 5432
