# Start by building the application.
FROM golang:1.17-bullseye

# Manually install libpcap because the static build fail with the one provided by distribution
ARG PCAPV=1.10.1
ADD http://www.tcpdump.org/release/libpcap-$PCAPV.tar.gz /tmp
RUN apt-get update && apt-get install -y make flex bison && \
    tar -xzf /tmp/libpcap-$PCAPV.tar.gz && \
    (cd libpcap-$PCAPV && ./configure --prefix=/usr --enable-bluetooth=no && make install) && \
    rm -r /tmp/* libpcap-$PCAPV
