FROM debian:bookworm-slim
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security
RUN sed -i "s|http://deb.debian.org/debian$|${DEBIAN_MIRROR}|; s|http://deb.debian.org/debian-security$|${DEBIAN_SECURITY_MIRROR}|" /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends python3 libsodium23 \
    && rm -rf /var/lib/apt/lists/*
COPY ssr_compat.py /opt/ssr-compat/sitecustomize.py
ENV PYTHONPATH=/opt/ssr-compat
