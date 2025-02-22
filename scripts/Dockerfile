# Start by building the application.
FROM docker.io/golang:1.24-bullseye

# Manually install libpcap because the static build fail with the one provided by distribution
ARG PCAPV=1.10.5
ADD http://www.tcpdump.org/release/libpcap-$PCAPV.tar.gz /tmp
RUN apt-get update && apt-get install -y make flex bison && \
    tar -xzf /tmp/libpcap-$PCAPV.tar.gz && \
    (cd libpcap-$PCAPV && ./configure --prefix=/usr --enable-bluetooth=no && make install) && \
    rm -r /tmp/* libpcap-$PCAPV
