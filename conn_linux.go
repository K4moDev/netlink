//go:build linux
// +build linux

package netlink

import (
	"context"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/mdlayher/socket"
	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

var _ Socket = &conn{}

// A conn is the Linux implementation of a netlink sockets connection.
type conn struct {
	s *socket.Conn
}

<<<<<<< HEAD
// A socket is an interface over socket system calls.
type socket interface {
	Bind(sa unix.Sockaddr) error
	Close() error
	FD() int
	File() *os.File
	Getsockname() (unix.Sockaddr, error)
	Recvmsg(p, oob []byte, flags int) (n int, oobn int, recvflags int, from unix.Sockaddr, err error)
	Sendmsg(p, oob []byte, to unix.Sockaddr, flags int) error
<<<<<<< HEAD
	SetSockoptSockFprog(level, opt int, fprog *unix.SockFprog) error
	SetSockoptInt(level, opt, value int) error
=======
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
<<<<<<< HEAD
	SetSockopt(level, name int, v unsafe.Pointer, l uint32) error
>>>>>>> 2a82be3 (netlink: enable usage of the network poller)
=======
	SetSockoptSockFprog(level, opt int, fprog *unix.SockFprog) error
	SetSockoptInt(level, opt, value int) error
	GetSockoptInt(level, opt int) (int, error)
>>>>>>> 5a2be09 (Improve behaviour of SetReadBuffer / SetWriteBuffer (#165))
}

=======
>>>>>>> beb09e3 (netlink: remove internal socket interface, rely more on integration tests)
// dial is the entry point for Dial. dial opens a netlink socket using
// system calls, and returns its PID.
func dial(family int, config *Config) (*conn, uint32, error) {
	if config == nil {
		config = &Config{}
	}

	// Prepare the netlink socket.
	s, err := socket.Socket(
		unix.AF_NETLINK,
		unix.SOCK_RAW,
		family,
		"netlink",
		&socket.Config{NetNS: config.NetNS},
	)
	if err != nil {
		return nil, 0, err
	}

	return newConn(s, config)
}

// newConn binds a connection to netlink using the input *socket.Conn.
func newConn(s *socket.Conn, config *Config) (*conn, uint32, error) {
	if config == nil {
		config = &Config{}
	}

	addr := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: config.Groups,
		Pid:    config.PID,
	}

	// Socket must be closed in the event of any system call errors, to avoid
	// leaking file descriptors.

	if err := s.Bind(addr); err != nil {
		_ = s.Close()
		return nil, 0, err
	}

	sa, err := s.Getsockname()
	if err != nil {
		_ = s.Close()
		return nil, 0, err
	}

	c := &conn{s: s}
	if config.Strict {
		// The caller has requested the strict option set. Historically we have
		// recommended checking for ENOPROTOOPT if the kernel does not support
		// the option in question, but that may result in a silent failure and
		// unexpected behavior for the user.
		//
		// Treat any error here as a fatal error, and require the caller to deal
		// with it.
		for _, o := range []ConnOption{ExtendedAcknowledge, GetStrictCheck} {
			if err := c.SetOption(o, true); err != nil {
				_ = c.Close()
				return nil, 0, err
			}
		}
	}

	return c, sa.(*unix.SockaddrNetlink).Pid, nil
}

// SendMessages serializes multiple Messages and sends them to netlink.
func (c *conn) SendMessages(messages []Message) error {
	var buf []byte
	for _, m := range messages {
		b, err := m.MarshalBinary()
		if err != nil {
			return err
		}

		buf = append(buf, b...)
	}

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	_, err := c.s.Sendmsg(context.Background(), buf, nil, sa, 0)
	return err
}

// Send sends a single Message to netlink.
func (c *conn) Send(m Message) error {
	b, err := m.MarshalBinary()
	if err != nil {
		return err
	}

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	_, err = c.s.Sendmsg(context.Background(), b, nil, sa, 0)
	return err
}

// Receive receives one or more Messages from netlink.
func (c *conn) Receive() ([]Message, error) {
	b := make([]byte, os.Getpagesize())
	for {
		// Peek at the buffer to see how many bytes are available.
		//
		// TODO(mdlayher): deal with OOB message data if available, such as
		// when PacketInfo ConnOption is true.
		n, _, _, _, err := c.s.Recvmsg(context.Background(), b, nil, unix.MSG_PEEK)
		if err != nil {
			return nil, err
		}

		// Break when we can read all messages
		if n < len(b) {
			break
		}

		// Double in size if not enough bytes
		b = make([]byte, len(b)*2)
	}

	// Read out all available messages
	n, _, _, _, err := c.s.Recvmsg(context.Background(), b, nil, 0)
	if err != nil {
		return nil, err
	}

	raw, err := syscall.ParseNetlinkMessage(b[:nlmsgAlign(n)])
	if err != nil {
		return nil, err
	}

	msgs := make([]Message, 0, len(raw))
	for _, r := range raw {
		m := Message{
			Header: sysToHeader(r.Header),
			Data:   r.Data,
		}

		msgs = append(msgs, m)
	}

	return msgs, nil
}

// Close closes the connection.
func (c *conn) Close() error { return c.s.Close() }

// JoinGroup joins a multicast group by ID.
func (c *conn) JoinGroup(group uint32) error {
	return c.s.SetsockoptInt(unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(group))
}

// LeaveGroup leaves a multicast group by ID.
func (c *conn) LeaveGroup(group uint32) error {
	return c.s.SetsockoptInt(unix.SOL_NETLINK, unix.NETLINK_DROP_MEMBERSHIP, int(group))
}

// SetBPF attaches an assembled BPF program to a conn.
func (c *conn) SetBPF(filter []bpf.RawInstruction) error { return c.s.SetBPF(filter) }

// RemoveBPF removes a BPF filter from a conn.
func (c *conn) RemoveBPF() error { return c.s.RemoveBPF() }

// SetOption enables or disables a netlink socket option for the Conn.
func (c *conn) SetOption(option ConnOption, enable bool) error {
	o, ok := linuxOption(option)
	if !ok {
		// Return the typical Linux error for an unknown ConnOption.
		return os.NewSyscallError("setsockopt", unix.ENOPROTOOPT)
	}

	var v int
	if enable {
		v = 1
	}

	return c.s.SetsockoptInt(unix.SOL_NETLINK, o, v)
}

func (c *conn) SetDeadline(t time.Time) error      { return c.s.SetDeadline(t) }
func (c *conn) SetReadDeadline(t time.Time) error  { return c.s.SetReadDeadline(t) }
func (c *conn) SetWriteDeadline(t time.Time) error { return c.s.SetWriteDeadline(t) }

// SetReadBuffer sets the size of the operating system's receive buffer
// associated with the Conn.
func (c *conn) SetReadBuffer(bytes int) error { return c.s.SetReadBuffer(bytes) }

// SetReadBuffer sets the size of the operating system's transmit buffer
// associated with the Conn.
func (c *conn) SetWriteBuffer(bytes int) error { return c.s.SetWriteBuffer(bytes) }

// SyscallConn returns a raw network connection.
func (c *conn) SyscallConn() (syscall.RawConn, error) { return c.s.SyscallConn() }

// linuxOption converts a ConnOption to its Linux value.
func linuxOption(o ConnOption) (int, bool) {
	switch o {
	case PacketInfo:
		return unix.NETLINK_PKTINFO, true
	case BroadcastError:
		return unix.NETLINK_BROADCAST_ERROR, true
	case NoENOBUFS:
		return unix.NETLINK_NO_ENOBUFS, true
	case ListenAllNSID:
		return unix.NETLINK_LISTEN_ALL_NSID, true
	case CapAcknowledge:
		return unix.NETLINK_CAP_ACK, true
	case ExtendedAcknowledge:
		return unix.NETLINK_EXT_ACK, true
	case GetStrictCheck:
		return unix.NETLINK_GET_STRICT_CHK, true
	default:
		return 0, false
	}
}

// sysToHeader converts a syscall.NlMsghdr to a Header.
func sysToHeader(r syscall.NlMsghdr) Header {
	// NB: the memory layout of Header and syscall.NlMsgHdr must be
	// exactly the same for this unsafe cast to work
	return *(*Header)(unsafe.Pointer(&r))
}

// newError converts an error number from netlink into the appropriate
// system call error for Linux.
func newError(errno int) error {
	return syscall.Errno(errno)
}
<<<<<<< HEAD

// A socket wraps system call operations.
type socket struct {
	// Atomics must come first.
	closed uint32

	fd *os.File
	rc syscall.RawConn
}

// read executes f, a read function, against the associated file descriptor.
func (s *socket) read(f func(fd int) bool) error {
	if atomic.LoadUint32(&s.closed) != 0 {
		return syscall.EBADF
	}

	return s.rc.Read(func(sysfd uintptr) bool {
		return f(int(sysfd))
	})
}

// write executes f, a write function, against the associated file descriptor.
func (s *socket) write(f func(fd int) bool) error {
	if atomic.LoadUint32(&s.closed) != 0 {
		return syscall.EBADF
	}

	return s.rc.Write(func(sysfd uintptr) bool {
		return f(int(sysfd))
	})
}

// control executes f, a control function, against the associated file descriptor.
func (s *socket) control(f func(fd int)) error {
	if atomic.LoadUint32(&s.closed) != 0 {
		return syscall.EBADF
	}

	return s.rc.Control(func(sysfd uintptr) {
		f(int(sysfd))
	})
}

func (s *socket) SyscallConn() (syscall.RawConn, error) {
	if atomic.LoadUint32(&s.closed) != 0 {
		return nil, syscall.EBADF
	}

	return s.rc, nil
}

func newSocket(family int) (*socket, error) {
	// Mirror what the standard library does when creating file
	// descriptors: avoid racing a fork/exec with the creation
	// of new file descriptors, so that child processes do not
	// inherit netlink socket file descriptors unexpectedly.
	//
	// On Linux, SOCK_CLOEXEC was introduced in 2.6.27. OTOH,
	// Go supports Linux 2.6.23 and above. If we get EINVAL on
	// the first try, it may be that we are running on a kernel
	// older than 2.6.27. In that case, take syscall.ForkLock
	// and try again without SOCK_CLOEXEC.
	//
	// For a more thorough explanation, see similar work in the
	// Go tree: func sysSocket in net/sock_cloexec.go, as well
	// as the detailed comment in syscall/exec_unix.go.
	fd, err := unix.Socket(
		unix.AF_NETLINK,
		unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC,
		family,
	)
	if err == unix.EINVAL {
		syscall.ForkLock.RLock()
		fd, err = unix.Socket(
			unix.AF_NETLINK,
			unix.SOCK_RAW,
			family,
		)
		if err == nil {
			unix.CloseOnExec(fd)
		}
		syscall.ForkLock.RUnlock()

		if err := unix.SetNonblock(fd, true); err != nil {
			return nil, err
		}
	}

	// os.NewFile registers the file descriptor with the runtime poller, which
	// is then used for most subsequent operations except those that require
	// raw I/O via SyscallConn.
	//
	// See also: https://golang.org/pkg/os/#NewFile
	f := os.NewFile(uintptr(fd), "netlink")
	rc, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}

	return &socket{
		fd: f,
		rc: rc,
	}, nil
}

func (s *socket) Bind(sa unix.Sockaddr) error {
	var err error
	doErr := s.control(func(fd int) {
		err = unix.Bind(fd, sa)
	})
	if doErr != nil {
		return doErr
	}

	return err
}

func (s *socket) Close() error {
	// The caller has expressed an intent to close the socket, so immediately
	// increment s.closed to force further calls to result in EBADF before also
	// closing the file descriptor to unblock any outstanding operations.
	//
	// Because other operations simply check for s.closed != 0, we will permit
	// double Close, which would increment s.closed beyond 1.
	if atomic.AddUint32(&s.closed, 1) != 1 {
		// Multiple Close calls.
		return nil
	}

	return s.fd.Close()
}

func (s *socket) Getsockname() (unix.Sockaddr, error) {
	var (
		sa  unix.Sockaddr
		err error
	)

	doErr := s.control(func(fd int) {
		sa, err = unix.Getsockname(fd)
	})
	if doErr != nil {
		return nil, doErr
	}

	return sa, err
}

func (s *socket) Recvmsg(p, oob []byte, flags int) (int, int, int, unix.Sockaddr, error) {
	var (
		n, oobn, recvflags int
		from               unix.Sockaddr
		err                error
	)

	doErr := s.read(func(fd int) bool {
		n, oobn, recvflags, from, err = unix.Recvmsg(fd, p, oob, flags)

		// Check for readiness.
		return ready(err)
	})
	if doErr != nil {
		return 0, 0, 0, nil, doErr
	}

	return n, oobn, recvflags, from, err
}

func (s *socket) Sendmsg(p, oob []byte, to unix.Sockaddr, flags int) error {
	var err error
	doErr := s.write(func(fd int) bool {
		err = unix.Sendmsg(fd, p, oob, to, flags)

		// Check for readiness.
		return ready(err)
	})
	if doErr != nil {
		return doErr
	}

	return err
}

<<<<<<< HEAD
<<<<<<< HEAD
func (s *sysSocket) SetSockoptInt(level, opt, value int) error {
	// Value must be in range of a C integer.
	if value < math.MinInt32 || value > math.MaxInt32 {
		return unix.EINVAL
	}

	var err error
	doErr := s.do(func() {
		err = unix.SetsockoptInt(s.fd, level, opt, int(value))
	})
	if doErr != nil {
		return doErr
	}

	return err
}

func (s *sysSocket) SetSockoptSockFprog(level, opt int, fprog *unix.SockFprog) error {
	var err error
	doErr := s.do(func() {
		err = unix.SetsockoptSockFprog(s.fd, level, opt, fprog)
=======
func (s *sysSocket) SetDeadline(t time.Time) error {
	return s.fd.SetDeadline(t)
}

func (s *sysSocket) SetReadDeadline(t time.Time) error {
	return s.fd.SetReadDeadline(t)
}

func (s *sysSocket) SetWriteDeadline(t time.Time) error {
	return s.fd.SetWriteDeadline(t)
}

func (s *sysSocket) SetSockopt(level, name int, v unsafe.Pointer, l uint32) error {
=======
func (s *socket) SetDeadline(t time.Time) error      { return s.fd.SetDeadline(t) }
func (s *socket) SetReadDeadline(t time.Time) error  { return s.fd.SetReadDeadline(t) }
func (s *socket) SetWriteDeadline(t time.Time) error { return s.fd.SetWriteDeadline(t) }

func (s *socket) SetSockoptInt(level, opt, value int) error {
	// Value must be in range of a C integer.
	if value < math.MinInt32 || value > math.MaxInt32 {
		return unix.EINVAL
	}

>>>>>>> beb09e3 (netlink: remove internal socket interface, rely more on integration tests)
	var err error
	doErr := s.control(func(fd int) {
<<<<<<< HEAD
		err = setsockopt(fd, level, name, v, l)
>>>>>>> 2a82be3 (netlink: enable usage of the network poller)
=======
		err = unix.SetsockoptInt(fd, level, opt, value)
	})
	if doErr != nil {
		return doErr
	}

	return err
}

func (s *socket) SetSockoptSockFprog(level, opt int, fprog *unix.SockFprog) error {
	var err error
	doErr := s.control(func(fd int) {
		err = unix.SetsockoptSockFprog(fd, level, opt, fprog)
>>>>>>> 5a2be09 (Improve behaviour of SetReadBuffer / SetWriteBuffer (#165))
	})
	if doErr != nil {
		return doErr
	}

	return err
}

// ready indicates readiness based on the value of err.
func ready(err error) bool {
	// When a socket is in non-blocking mode, we might see
	// EAGAIN. In that case, return false to let the poller wait for readiness.
	// See the source code for internal/poll.FD.RawRead for more details.
	//
	// Starting in Go 1.14, goroutines are asynchronously preemptible. The 1.14
	// release notes indicate that applications should expect to see EINTR more
	// often on slow system calls (like recvmsg while waiting for input), so
	// we must handle that case as well.
	//
	// If the socket is in blocking mode, EAGAIN should never occur.
	switch err {
	case syscall.EAGAIN, syscall.EINTR:
		// Not ready.
		return false
	default:
		// Ready whether there was error or no error.
		return true
	}
}
=======
>>>>>>> 3481a01 (netlink: replace custom syscall/netpoll integration with *socket.Conn)
