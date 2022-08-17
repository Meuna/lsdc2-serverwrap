# Start by building the application.
FROM golang:1.17-bullseye as build

# Manually install libpcap because the static build fail with the one provided by distribution
ARG PCAPV=1.10.1
WORKDIR /tmp/pcap
ADD http://www.tcpdump.org/release/libpcap-$PCAPV.tar.gz /tmp/pcap
RUN apt-get update; apt-get install -y make flex bison
RUN tar -xzf libpcap-$PCAPV.tar.gz \
    && cd libpcap-$PCAPV \
    && ./configure --prefix=/usr --enable-bluetooth=no \
    && make install

# Now build the app
WORKDIR /go/src/app
ADD . /go/src/app

RUN go get -d -v ./...

RUN go build --ldflags "-w -s -extldflags \"-static\"" -o /go/bin/serverwrap .

# Now copy it into our base image.
FROM alpine
COPY --from=build /go/bin/serverwrap /
CMD ["cp", "/serverwrap", "/mnt"]
