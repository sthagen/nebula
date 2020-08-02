package nebula

import (
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

type Tun struct {
	io.ReadWriteCloser
	fd           int
	Device       string
	Cidr         *net.IPNet
	MaxMTU       int
	DefaultMTU   int
	TXQueueLen   int
	Routes       []route
	UnsafeRoutes []route
}

func newTunFromFd(deviceFd int, cidr *net.IPNet, defaultMTU int, routes []route, unsafeRoutes []route, txQueueLen int) (ifce *Tun, err error) {
	file := os.NewFile(uintptr(deviceFd), "/dev/net/tun")

	ifce = &Tun{
		ReadWriteCloser: file,
		fd:              int(file.Fd()),
		Device:          "android",
		Cidr:            cidr,
		DefaultMTU:      defaultMTU,
		TXQueueLen:      txQueueLen,
		Routes:          routes,
		UnsafeRoutes:    unsafeRoutes,
	}
	return
}

func newTun(deviceName string, cidr *net.IPNet, defaultMTU int, routes []route, unsafeRoutes []route, txQueueLen int) (ifce *Tun, err error) {
	return nil, fmt.Errorf("newTun not supported in Android")
}

func (c *Tun) WriteRaw(b []byte) error {
	var nn int
	for {
		max := len(b)
		n, err := unix.Write(c.fd, b[nn:max])
		if n > 0 {
			nn += n
		}
		if nn == len(b) {
			return err
		}

		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrUnexpectedEOF
		}
	}
}

func (c Tun) Activate() error {
	return nil
}

func (c *Tun) CidrNet() *net.IPNet {
	return c.Cidr
}

func (c *Tun) DeviceName() string {
	return c.Device
}
