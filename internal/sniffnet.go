package internal

import (
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"
	"unsafe"
)

/*
#cgo LDFLAGS: -lpcap
#include <stdlib.h>
#include <pcap.h>
#include <linux/filter.h>
#include <sys/socket.h>
#include <poll.h>
*/
import "C"

const MTU = 128

// GetFirstIfaceWithIp4 return the first interface that is not a loopback
// and its IP4 address
func GetFirstIfaceWithIp4() (string, net.IP, error) {
	errorBuf := (*C.char)(C.calloc(C.PCAP_ERRBUF_SIZE, 1))
	defer C.free(unsafe.Pointer(errorBuf))
	var alldevs *C.pcap_if_t
	defer C.pcap_freealldevs(alldevs)

	if C.pcap_findalldevs(&alldevs, errorBuf) < 0 {
		return "", nil, errors.New(C.GoString(errorBuf))
	}

	d := alldevs
	for d != nil {
		iface := C.GoString(d.name)
		if iface != "lo" {
			a := d.addresses
			for a != nil {
				if a.addr.sa_family == syscall.AF_INET {
					ip4 := ntoaIP4(a.addr)
					if !ip4.IsLinkLocalUnicast() {
						return iface, ip4, nil
					}
				}
				a = a.next
			}
		}
		d = d.next
	}

	return "", nil, errors.New("no iface with AF_INET address found")
}

func ntoaIP4(a *C.struct_sockaddr) net.IP {
	ip := make([]byte, 4)
	goa := (*syscall.RawSockaddrInet4)(unsafe.Pointer(a))
	for i := 0; i < len(ip); i++ {
		ip[i] = goa.Addr[i]
	}
	return net.IP(ip)
}

// Open a raw socket (L2), configure it with a filter and poll it for the duration
// in argument. Return true if the polling succeeded; return false if the polling
// timedout
func PollFilteredIface(iface string, filter string, timeout time.Duration) (bool, error) {
	// L2 socket initialisation
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(C.htons(syscall.ETH_P_ALL)))
	if err != nil {
		return false, fmt.Errorf("syscall.Socket / %w", err)
	}
	defer syscall.Close(fd)

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 0); err != nil {
		return false, fmt.Errorf("syscall.SetsockoptInt / %w", err)
	}

	applyBPFFilter(fd, iface, filter)

	if err := syscall.BindToDevice(fd, iface); err != nil {
		return false, fmt.Errorf("syscall.BindToDevice / %w", err)
	}

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, MTU); err != nil {
		return false, fmt.Errorf("syscall.SetsockoptInt / %w", err)
	}

	// Empty the socket buffer in case some packet arrived while we were applying the filter
	buffer := make([]byte, MTU)
	for {
		_, _, err := syscall.Recvfrom(fd, buffer, syscall.MSG_DONTWAIT)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				// No more data to read
				break
			}
			return false, fmt.Errorf("syscall.Recvfrom / %w", err)
		}
	}

	// Polling is here
	msec := timeout.Milliseconds()
	pfd := C.struct_pollfd{fd: C.int(fd), events: C.POLLIN}
	C.poll(&pfd, 1, C.int(msec))

	packetsFound := (pfd.events & pfd.revents) > 0
	return packetsFound, nil
}

// Compile and apply a filter to the socket in argument.
func applyBPFFilter(fd int, iface string, filter string) {
	// Inspired by gopacket module, not clear why the mask is needed
	_, maskp, err := pcapLookupnet(iface)
	if err != nil {
		maskp = uint32(C.PCAP_NETMASK_UNKNOWN)
	}

	pcap_bpf, err := pcapCompile(filter, maskp)
	defer C.pcap_freecode((*C.struct_bpf_program)(&pcap_bpf))
	if err != nil {
		log.Panic(err)
	}

	// The application of the filter happens here, using low level functions.
	bpf := C.struct_sock_fprog{
		len:    (C.ushort)(pcap_bpf.bf_len),
		filter: (*C.struct_sock_filter)(pcap_bpf.bf_insns),
	}
	C.setsockopt(C.int(fd), C.SOL_SOCKET, C.SO_ATTACH_FILTER, unsafe.Pointer(&bpf), C.sizeof_struct_sock_fprog)
}

// "Inspired" by the gopacket library. Return some stuff, including maskp which
// is needed to compute a bpf program.
func pcapLookupnet(device string) (netp uint32, maskp uint32, err error) {
	errorBuf := (*C.char)(C.calloc(C.PCAP_ERRBUF_SIZE, 1))
	defer C.free(unsafe.Pointer(errorBuf))
	dev := C.CString(device)
	defer C.free(unsafe.Pointer(dev))
	if C.pcap_lookupnet(
		dev,
		(*C.bpf_u_int32)(unsafe.Pointer(&netp)),
		(*C.bpf_u_int32)(unsafe.Pointer(&maskp)),
		errorBuf,
	) < 0 {
		return 0, 0, errors.New(C.GoString(errorBuf))
	}
	return
}

// Compile and return a pcap bpf program. The caller has the responsability to
// free the program using C.pcap_freecode.
//
// Example:
//
//	bpf, err := pcapCompile(device, filter, maskp)
//	defer C.pcap_freecode((*C.struct_bpf_program)(&bpf))
func pcapCompile(filter string, maskp uint32) (C.struct_bpf_program, error) {
	// DLT_EN10MB is for Ethernet, which is assumed here
	p := C.pcap_open_dead(C.DLT_EN10MB, C.int(MTU))
	defer C.pcap_close(p)

	var bpf C.struct_bpf_program
	cfilter := C.CString(filter)
	defer C.free(unsafe.Pointer(cfilter))

	if C.pcap_compile(p, (*C.struct_bpf_program)(&bpf), cfilter, 1, C.bpf_u_int32(maskp)) < 0 {
		return bpf, errors.New(C.GoString(C.pcap_geterr(p)))
	}
	return bpf, nil
}
