package defs

const (
	EPERM         Err_t = 1
	ENOENT        Err_t = 2
	ESRCH         Err_t = 3
	EINTR         Err_t = 4
	EIO           Err_t = 5
	E2BIG         Err_t = 7
	EBADF         Err_t = 9
	ECHILD        Err_t = 10
	EAGAIN        Err_t = 11
	EWOULDBLOCK         = EAGAIN
	ENOMEM        Err_t = 12
	EACCES        Err_t = 13
	EFAULT        Err_t = 14
	EBUSY         Err_t = 16
	EEXIST        Err_t = 17
	ENODEV        Err_t = 19
	ENOTDIR       Err_t = 20
	EISDIR        Err_t = 21
	EINVAL        Err_t = 22
	EMFILE        Err_t = 24
	ENOSPC        Err_t = 28
	ESPIPE        Err_t = 29
	EPIPE         Err_t = 32
	ERANGE        Err_t = 34
	ENAMETOOLONG  Err_t = 36
	ENOSYS        Err_t = 38
	ENOTEMPTY     Err_t = 39
	EDESTADDRREQ  Err_t = 40
	EAFNOSUPPORT  Err_t = 47
	EADDRINUSE    Err_t = 48
	EADDRNOTAVAIL Err_t = 49
	ENETDOWN      Err_t = 50
	ENETUNREACH   Err_t = 51
	EHOSTUNREACH  Err_t = 65
	ENOTSOCK      Err_t = 88
	EMSGSIZE      Err_t = 90
	EOPNOTSUPP    Err_t = 95
	ECONNRESET    Err_t = 104
	EISCONN       Err_t = 106
	ENOTCONN      Err_t = 107
	ETIMEDOUT     Err_t = 110
	ECONNREFUSED  Err_t = 111
	EINPROGRESS   Err_t = 115
	ENOHEAP       Err_t = 511
)

type Err_t int
