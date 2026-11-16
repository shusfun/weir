FROM debian:bookworm-slim
ARG FIREWALL_TOOL
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security
RUN case "$FIREWALL_TOOL" in ufw|firewalld|iptables) ;; *) exit 1 ;; esac \
    && sed -i "s|http://deb.debian.org/debian$|${DEBIAN_MIRROR}|; s|http://deb.debian.org/debian-security$|${DEBIAN_SECURITY_MIRROR}|" /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends "$FIREWALL_TOOL" iptables dbus iproute2 ebtables ipset \
    && rm -rf /var/lib/apt/lists/*

# This runtime lacks nftables inet/bridge NAT and IPv6 legacy support. firewalld
# uses its iptables backend with v4 legacy and v6 nft in the isolated namespace.
RUN if [ "$FIREWALL_TOOL" = firewalld ]; then \
      sed -i 's/^FirewallBackend=.*/FirewallBackend=iptables/' /etc/firewalld/firewalld.conf \
      && update-alternatives --set iptables /usr/sbin/iptables-legacy \
      && update-alternatives --set ip6tables /usr/sbin/ip6tables-nft \
      && update-alternatives --set ebtables /usr/sbin/ebtables-legacy; \
    fi
