# 门卫镜像：只有静态编译的门卫二进制和上游 TLS 校验需要的公共根证书。
# 基础镜像来自 AWS 上的 Docker 官方镜像（Docker Hub 在部分网络不可用），并固定摘要。
FROM public.ecr.aws/docker/library/debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY gatekeeper /usr/local/bin/aiofcage-gatekeeper
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/aiofcage-gatekeeper"]
