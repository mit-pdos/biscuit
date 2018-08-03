package bounds

import "runtime"

import "res"

type Boundkey_t int

const (
	B_ASPACE_T_K2USER_INNER Boundkey_t = iota
	B_ASPACE_T_USER2K_INNER
	B_BITMAP_T_APPLY
	B_ELF_T_ELF_LOAD
	B_FS_T_FS_NAMEI
	B_FS_T_FS_OP_RENAME
	B_FS_T__ISANCESTOR
	B_FUTEX_T_FUTEX_START
	B_IMEMNODE_T_BMAPFILL
	B_IMEMNODE_T__DESCAN
	B_IMEMNODE_T_DO_WRITE
	B_IMEMNODE_T_IFREE
	B_IMEMNODE_T_IMMAPINFO
	B_IMEMNODE_T_IREAD
	B_IMEMNODE_T_IWRITE
	B_IXGBE_T_INT_HANDLER
	B_KBD_DAEMON
	B_LOG_T_COMMITTER
	B_PIPEFOPS_T_WRITE
	B_PIPE_T_OP_FDADD
	B_PROC_T_RUN1
	B_PROC_T_USERARGS
	B_RAWDFOPS_T_READ
	B_RAWDFOPS_T_WRITE
	B_SYS_ACCEPT
	B_SYS_ACCESS
	B_SYS_BIND
	B_SYSCALL_T_SYS_CLOSE
	B_SYSCALL_T_SYS_EXIT
	B_SYS_CHDIR
	B_SYS_CONNECT
	B_SYS_DUP2
	B_SYS_EXECV
	B_SYS_FCNTL
	B_SYS_FORK
	B_SYS_FSTAT
	B_SYS_FTRUNCATE
	B_SYS_FUTEX
	B_SYS_GETCWD
	B_SYS_GETPID
	B_SYS_GETPPID
	B_SYS_GETRLIMIT
	B_SYS_GETRUSAGE
	B_SYS_GETSOCKOPT
	B_SYS_GETTID
	B_SYS_GETTIMEOFDAY
	B_SYS_INFO
	B_SYS_KILL
	B_SYS_LINK
	B_SYS_LISTEN
	B_SYS_LSEEK
	B_SYS_MKDIR
	B_SYS_MKNOD
	B_SYS_MMAP
	B_SYS_MUNMAP
	B_SYS_NANOSLEEP
	B_SYS_OPEN
	B_SYS_PAUSE
	B_SYS_PIPE2
	B_SYS_POLL
	B_SYS_PREAD
	B_SYS_PROF
	B_SYS_PWRITE
	B_SYS_READ
	B_SYS_READV
	B_SYS_REBOOT
	B_SYS_RECVFROM
	B_SYS_RECVMSG
	B_SYS_RENAME
	B_SYS_SENDMSG
	B_SYS_SENDTO
	B_SYS_SETRLIMIT
	B_SYS_SETSOCKOPT
	B_SYS_SHUTDOWN
	B_SYS_SIGACTION
	B_SYS_SOCKET
	B_SYS_SOCKETPAIR
	B_SYS_STAT
	B_SYS_SYNC
	B_SYS_THREXIT
	B_SYS_TRUNCATE
	B_SYS_UNLINK
	B_SYS_WAIT4
	B_SYS_WRITE
	B_SYS_WRITEV
	B_TCPFOPS_T_READ
	B_TCPFOPS_T_WRITE
	B_TCPTIMERS_T__TCPTIMERS_DAEMON
	B_USERBUF_T__TX
	B_USERIOVEC_T_IOV_INIT
	B_USERIOVEC_T__TX
)

func Bounds(k Boundkey_t) *res.Res_t {
	return boundres[k]
}

var boundres = []*res.Res_t {
	B_ASPACE_T_K2USER_INNER: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_ASPACE_T_K2USER_INNER]))}},
	B_ASPACE_T_USER2K_INNER: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_ASPACE_T_USER2K_INNER]))}},
	B_BITMAP_T_APPLY: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_BITMAP_T_APPLY]))}},
	B_ELF_T_ELF_LOAD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_ELF_T_ELF_LOAD]))}},
	B_FS_T_FS_NAMEI: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_FS_T_FS_NAMEI]))}},
	B_FS_T_FS_OP_RENAME: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_FS_T_FS_OP_RENAME]))}},
	B_FS_T__ISANCESTOR: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_FS_T__ISANCESTOR]))}},
	B_FUTEX_T_FUTEX_START: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_FUTEX_T_FUTEX_START]))}},
	B_IMEMNODE_T_BMAPFILL: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_BMAPFILL]))}},
	B_IMEMNODE_T__DESCAN: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T__DESCAN]))}},
	B_IMEMNODE_T_DO_WRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_DO_WRITE]))}},
	B_IMEMNODE_T_IFREE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_IFREE]))}},
	B_IMEMNODE_T_IMMAPINFO: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_IMMAPINFO]))}},
	B_IMEMNODE_T_IREAD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_IREAD]))}},
	B_IMEMNODE_T_IWRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IMEMNODE_T_IWRITE]))}},
	B_IXGBE_T_INT_HANDLER: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_IXGBE_T_INT_HANDLER]))}},
	B_KBD_DAEMON: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_KBD_DAEMON]))}},
	B_LOG_T_COMMITTER: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_LOG_T_COMMITTER]))}},
	B_PIPEFOPS_T_WRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_PIPEFOPS_T_WRITE]))}},
	B_PIPE_T_OP_FDADD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_PIPE_T_OP_FDADD]))}},
	B_PROC_T_RUN1: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_PROC_T_RUN1]))}},
	B_PROC_T_USERARGS: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_PROC_T_USERARGS]))}},
	B_RAWDFOPS_T_READ: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_RAWDFOPS_T_READ]))}},
	B_RAWDFOPS_T_WRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_RAWDFOPS_T_WRITE]))}},
	B_SYS_ACCEPT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_ACCEPT]))}},
	B_SYS_ACCESS: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_ACCESS]))}},
	B_SYS_BIND: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_BIND]))}},
	B_SYSCALL_T_SYS_CLOSE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYSCALL_T_SYS_CLOSE]))}},
	B_SYSCALL_T_SYS_EXIT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYSCALL_T_SYS_EXIT]))}},
	B_SYS_CHDIR: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_CHDIR]))}},
	B_SYS_CONNECT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_CONNECT]))}},
	B_SYS_DUP2: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_DUP2]))}},
	B_SYS_EXECV: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_EXECV]))}},
	B_SYS_FCNTL: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_FCNTL]))}},
	B_SYS_FORK: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_FORK]))}},
	B_SYS_FSTAT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_FSTAT]))}},
	B_SYS_FTRUNCATE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_FTRUNCATE]))}},
	B_SYS_FUTEX: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_FUTEX]))}},
	B_SYS_GETCWD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETCWD]))}},
	B_SYS_GETPID: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETPID]))}},
	B_SYS_GETPPID: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETPPID]))}},
	B_SYS_GETRLIMIT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETRLIMIT]))}},
	B_SYS_GETRUSAGE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETRUSAGE]))}},
	B_SYS_GETSOCKOPT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETSOCKOPT]))}},
	B_SYS_GETTID: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETTID]))}},
	B_SYS_GETTIMEOFDAY: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_GETTIMEOFDAY]))}},
	B_SYS_INFO: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_INFO]))}},
	B_SYS_KILL: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_KILL]))}},
	B_SYS_LINK: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_LINK]))}},
	B_SYS_LISTEN: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_LISTEN]))}},
	B_SYS_LSEEK: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_LSEEK]))}},
	B_SYS_MKDIR: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_MKDIR]))}},
	B_SYS_MKNOD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_MKNOD]))}},
	B_SYS_MMAP: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_MMAP]))}},
	B_SYS_MUNMAP: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_MUNMAP]))}},
	B_SYS_NANOSLEEP: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_NANOSLEEP]))}},
	B_SYS_OPEN: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_OPEN]))}},
	B_SYS_PAUSE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_PAUSE]))}},
	B_SYS_PIPE2: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_PIPE2]))}},
	B_SYS_POLL: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_POLL]))}},
	B_SYS_PREAD: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_PREAD]))}},
	B_SYS_PROF: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_PROF]))}},
	B_SYS_PWRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_PWRITE]))}},
	B_SYS_READ: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_READ]))}},
	B_SYS_READV: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_READV]))}},
	B_SYS_REBOOT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_REBOOT]))}},
	B_SYS_RECVFROM: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_RECVFROM]))}},
	B_SYS_RECVMSG: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_RECVMSG]))}},
	B_SYS_RENAME: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_RENAME]))}},
	B_SYS_SENDMSG: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SENDMSG]))}},
	B_SYS_SENDTO: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SENDTO]))}},
	B_SYS_SETRLIMIT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SETRLIMIT]))}},
	B_SYS_SETSOCKOPT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SETSOCKOPT]))}},
	B_SYS_SHUTDOWN: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SHUTDOWN]))}},
	B_SYS_SIGACTION: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SIGACTION]))}},
	B_SYS_SOCKET: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SOCKET]))}},
	B_SYS_SOCKETPAIR: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SOCKETPAIR]))}},
	B_SYS_STAT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_STAT]))}},
	B_SYS_SYNC: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_SYNC]))}},
	B_SYS_THREXIT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_THREXIT]))}},
	B_SYS_TRUNCATE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_TRUNCATE]))}},
	B_SYS_UNLINK: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_UNLINK]))}},
	B_SYS_WAIT4: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_WAIT4]))}},
	B_SYS_WRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_WRITE]))}},
	B_SYS_WRITEV: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_SYS_WRITEV]))}},
	B_TCPFOPS_T_READ: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_TCPFOPS_T_READ]))}},
	B_TCPFOPS_T_WRITE: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_TCPFOPS_T_WRITE]))}},
	B_TCPTIMERS_T__TCPTIMERS_DAEMON: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_TCPTIMERS_T__TCPTIMERS_DAEMON]))}},
	B_USERBUF_T__TX: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_USERBUF_T__TX]))}},
	B_USERIOVEC_T_IOV_INIT: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_USERIOVEC_T_IOV_INIT]))}},
	B_USERIOVEC_T__TX: &res.Res_t{Objs: runtime.Resobjs_t{1: uint32(uint(bounds[B_USERIOVEC_T__TX]))}},
}

var bounds = []int{
	B_ASPACE_T_K2USER_INNER: 1 * 824 + 13 * 24 + 1 * 4096 + 1 * 8 + 1 * 1 + 32 * 48 + 3 * 64 + 1 * 20 + 80 * 40 + 11 * 120 + 17 * 216 + 116 * 32 + 13 * 16,
	B_ASPACE_T_USER2K_INNER: 17 * 216 + 11 * 120 + 116 * 32 + 1 * 4096 + 3 * 64 + 1 * 20 + 1 * 1 + 13 * 16 + 13 * 24 + 1 * 824 + 80 * 40 + 32 * 48 + 1 * 8,
	B_BITMAP_T_APPLY: 4 * 40 + 1 * 1 + 2 * 32 + 1 * 216 + 1 * 20 + 1 * 24 + 1 * 16 + 3 * 48 + 3 * 64,
	B_ELF_T_ELF_LOAD: 72 * 216 + 4 * 824 + 44 * 120 + 1 * 4096 + 52 * 16 + 325 * 40 + 455 * 32 + 4 * 112 + 1 * 8 + 130 * 48 + 1 * 504 + 1 * 1 + 1 * 20 + 52 * 24 + 3 * 64,
	B_FS_T_FS_NAMEI: 187 * 14 + 3 * 8 + 318 * 32 + 15 * 16 + 410 * 48 + 3 * 1 + 87 * 40 + 19 * 216 + 3 * 824 + 1 * 4096 + 3 * 64 + 11 * 120 + 16 * 24 + 1 * 20,
	B_FS_T_FS_OP_RENAME: 2478 * 40 + 507 * 216 + 2806 * 32 + 3553 * 14 + 1 * 4096 + 3 * 64 + 1343 * 16 + 24 * 824 + 3 * 1 + 8030 * 48 + 369 * 120 + 7 * 8 + 1 * 20 + 3 * 2 + 4 * 56 + 404 * 24,
	B_FS_T__ISANCESTOR: 1 * 4096 + 1 * 20 + 81 * 40 + 17 * 216 + 11 * 120 + 216 * 32 + 15 * 24 + 187 * 14 + 3 * 1 + 2 * 824 + 406 * 48 + 14 * 16 + 1 * 2 + 3 * 8 + 3 * 64,
	B_FUTEX_T_FUTEX_START: 4 * 24 + 1 * 32 + 3 * 424 + 2 * 232 + 1 * 400 + 1 * 8 + 1 * 40 + 1 * 80 + 3 * 104,
	B_IMEMNODE_T_BMAPFILL: 69 * 40 + 14 * 32 + 3 * 64 + 1 * 20 + 26 * 48 + 11 * 120 + 11 * 24 + 14 * 216 + 11 * 16 + 1 * 4096 + 1 * 8 + 1 * 1,
	B_IMEMNODE_T__DESCAN: 15 * 216 + 187 * 14 + 1 * 8 + 402 * 48 + 11 * 120 + 13 * 24 + 12 * 16 + 15 * 32 + 1 * 1 + 73 * 40 + 1 * 4096 + 3 * 64 + 1 * 20,
	B_IMEMNODE_T_DO_WRITE: 93 * 48 + 243 * 32 + 1 * 8 + 240 * 40 + 39 * 24 + 2 * 824 + 1 * 4096 + 1 * 1 + 50 * 216 + 39 * 16 + 1 * 96 + 3 * 64 + 35 * 120 + 1 * 20,
	B_IMEMNODE_T_IFREE: 437 * 40 + 3 * 64 + 1 * 20 + 14 * 120 + 104 * 16 + 1 * 90 + 104 * 32 + 102 * 24 + 1 * 1 + 205 * 48 + 102 * 216,
	B_IMEMNODE_T_IMMAPINFO: 28 * 48 + 12 * 16 + 12 * 24 + 1 * 4096 + 1 * 8 + 1 * 1 + 3 * 64 + 11 * 120 + 15 * 32 + 74 * 40 + 15 * 216 + 1 * 20,
	B_IMEMNODE_T_IREAD: 38 * 16 + 49 * 216 + 90 * 48 + 33 * 120 + 2 * 824 + 38 * 24 + 1 * 20 + 1 * 112 + 242 * 32 + 1 * 8 + 232 * 40 + 1 * 4096 + 1 * 1 + 3 * 64,
	B_IMEMNODE_T_IWRITE: 1 * 8 + 90 * 48 + 242 * 32 + 234 * 40 + 1 * 20 + 34 * 120 + 1 * 96 + 38 * 16 + 1 * 1 + 3 * 64 + 49 * 216 + 38 * 24 + 2 * 824 + 1 * 4096,
	B_IXGBE_T_INT_HANDLER: 256 * 568 + 256 * 608 + 256 * 12 + 1 * 64 + 3 * 280 + 512 * 56 + 2 * 1024 + 1 * 1524 + 2 * 48 + 1 * 16 + 256 * 32,
	B_KBD_DAEMON: 4 * 1048 + 7 * 24 + 1 * 8 + 1 * 10 + 1 * 1 + 1 * 16 + 2 * 32 + 1 * 240,
	B_LOG_T_COMMITTER: 512 * 120 + 1 * 8216 + 2 * 56 + 4 * 64 + 1 * 20 + 2 * 27000 + 4035 * 24 + 4044 * 16 + 3 * 9216 + 4043 * 48 + 4038 * 32 + 2 * 96 + 2 * 8 + 18612 * 40 + 2 * 216,
	B_PIPEFOPS_T_WRITE: 4 * 824 + 317 * 40 + 456 * 32 + 1 * 8 + 3 * 64 + 1 * 20 + 44 * 120 + 125 * 48 + 52 * 24 + 68 * 216 + 1 * 4096 + 1 * 1 + 52 * 16,
	B_PIPE_T_OP_FDADD: 1 * 80,
	B_PROC_T_RUN1: 1 * 20 + 26 * 24 + 22 * 120 + 4 * 64 + 1 * 8 + 34 * 216 + 1 * 512 + 2 * 824 + 26 * 16 + 229 * 32 + 1 * 4096 + 63 * 48 + 159 * 40 + 1 * 1,
	B_PROC_T_USERARGS: 33 * 120 + 51 * 216 + 238 * 40 + 3 * 824 + 4 * 8 + 351 * 32 + 94 * 48 + 39 * 16 + 1 * 4096 + 1 * 20 + 10 * 1 + 3 * 536 + 1 * 288 + 41 * 24 + 3 * 64 + 1 * 1560,
	B_RAWDFOPS_T_READ: 231 * 32 + 27 * 24 + 1 * 8 + 1 * 1 + 1 * 20 + 163 * 40 + 22 * 120 + 35 * 216 + 2 * 824 + 1 * 4096 + 3 * 64 + 27 * 16 + 65 * 48,
	B_RAWDFOPS_T_WRITE: 34 * 216 + 2 * 824 + 28 * 16 + 1 * 1 + 1 * 20 + 165 * 40 + 28 * 24 + 65 * 48 + 23 * 120 + 232 * 32 + 1 * 4096 + 1 * 8 + 3 * 64,
	B_SYS_ACCEPT: 85 * 216 + 55 * 120 + 66 * 16 + 66 * 24 + 1 * 20 + 5 * 824 + 1 * 4096 + 1 * 1 + 3 * 64 + 396 * 40 + 1 * 4120 + 156 * 48 + 570 * 32 + 1 * 8,
	B_SYS_ACCESS: 1376 * 48 + 3 * 1 + 3 * 536 + 109 * 24 + 95 * 120 + 3 * 8 + 1 * 4096 + 3 * 64 + 295 * 16 + 659 * 40 + 1 * 20 + 9 * 824 + 1011 * 32 + 137 * 216 + 561 * 14,
	B_SYS_BIND: 1345 * 48 + 898 * 32 + 1 * 208 + 84 * 120 + 3 * 1 + 561 * 14 + 3 * 8 + 1 * 56 + 282 * 16 + 1 * 1656 + 8 * 824 + 96 * 24 + 1 * 280 + 1 * 4096 + 3 * 64 + 580 * 40 + 120 * 216 + 1 * 20,
	B_SYSCALL_T_SYS_CLOSE: 1 * 24 + 2 * 56 + 1 * 144,
	B_SYSCALL_T_SYS_EXIT: 2 * 24 + 1 * 8 + 2 * 56 + 1 * 144,
	B_SYS_CHDIR: 295 * 16 + 110 * 24 + 561 * 14 + 3 * 64 + 659 * 40 + 95 * 120 + 3 * 8 + 1011 * 32 + 9 * 824 + 1 * 20 + 137 * 216 + 4 * 536 + 3 * 1 + 1 * 4096 + 1377 * 48,
	B_SYS_CONNECT: 36 * 120 + 3 * 56 + 187 * 14 + 1 * 72 + 1 * 280 + 602 * 40 + 529 * 32 + 1 * 200 + 644 * 48 + 138 * 216 + 130 * 16 + 4 * 824 + 131 * 24 + 1 * 12 + 1 * 96 + 1 * 8192,
	B_SYS_DUP2: 2 * 24 + 1 * 40 + 1 * 48 + 1 * 216 + 2 * 56 + 1 * 144,
	B_SYS_EXECV: 1 * 4096 + 1 * 288 + 1786 * 48 + 561 * 14 + 4 * 8 + 1 * 240 + 1 * 10 + 4 * 1048 + 365 * 216 + 1703 * 40 + 1 * 1560 + 1 * 56 + 3 * 64 + 464 * 16 + 2480 * 32 + 279 * 24 + 7 * 112 + 1 * 512 + 1 * 1 + 1 * 20 + 6 * 536 + 238 * 120 + 22 * 824,
	B_SYS_FCNTL: 0,
	B_SYS_FORK: (1554) * 216 + (1554) * 40 + (1554) * 48 + (512) * 24 + (1024) * 40 + (1024) * 112 + 2 * 1 + 63 * 40 + 14 * 48 + 1 * 1600 + 1 * 192 + 2 * 8 + 13 * 16 + 1 * 4120 + 114 * 32 + 6 * 56 + 1 * 376 + 14 * 24 + 1 * 824 + 11 * 120 + 1 * 144,
	B_SYS_FSTAT: 2 * 824 + 1 * 1 + 1 * 20 + 36 * 48 + 19 * 216 + 11 * 120 + 3 * 64 + 1 * 72 + 217 * 32 + 14 * 24 + 1 * 4096 + 14 * 16 + 86 * 40 + 1 * 8,
	B_SYS_FTRUNCATE: 32 * 48 + 1 * 824 + 13 * 16 + 13 * 24 + 12 * 120 + 1 * 1 + 1 * 20 + 117 * 32 + 81 * 40 + 17 * 216 + 1 * 4096 + 1 * 8 + 3 * 64,
	B_SYS_FUTEX: 1 * 4096 + 2 * 81920 + 318 * 40 + 1 * 80 + 125 * 48 + 1 * 400 + 3 * 64 + 68 * 216 + 4 * 824 + 56 * 24 + 1 * 232 + 1 * 20 + 3 * 424 + 3 * 104 + 44 * 120 + 1 * 1 + 457 * 32 + 52 * 16 + 2 * 8,
	B_SYS_GETCWD: 63 * 48 + 22 * 120 + 1 * 4096 + 1 * 20 + 2 * 824 + 26 * 24 + 1 * 8 + 230 * 32 + 26 * 16 + 34 * 216 + 159 * 40 + 2 * 1 + 3 * 64,
	B_SYS_GETPID: 0,
	B_SYS_GETPPID: 0,
	B_SYS_GETRLIMIT: 44 * 120 + 52 * 24 + 1 * 1 + 1 * 4096 + 1 * 8 + 125 * 48 + 455 * 32 + 317 * 40 + 4 * 824 + 68 * 216 + 52 * 16 + 3 * 64 + 1 * 20,
	B_SYS_GETRUSAGE: 13 * 16 + 116 * 32 + 1 * 56 + 1 * 824 + 1 * 20 + 32 * 48 + 80 * 40 + 17 * 216 + 14 * 24 + 1 * 8 + 11 * 120 + 1 * 4096 + 1 * 1 + 3 * 64,
	B_SYS_GETSOCKOPT: 3 * 64 + 569 * 32 + 65 * 16 + 5 * 824 + 65 * 24 + 55 * 120 + 85 * 216 + 2 * 8 + 396 * 40 + 156 * 48 + 1 * 4096 + 1 * 1 + 1 * 20,
	B_SYS_GETTID: 0,
	B_SYS_GETTIMEOFDAY: 3 * 64 + 1 * 824 + 13 * 24 + 17 * 216 + 1 * 4096 + 13 * 16 + 1 * 8 + 1 * 1 + 1 * 20 + 32 * 48 + 116 * 32 + 81 * 40 + 11 * 120,
	B_SYS_INFO: 1 * 5776 + 1 * 32,
	B_SYS_KILL: 0,
	B_SYS_LINK: 2014 * 48 + 6 * 536 + 748 * 14 + 3 * 1 + 1 * 4096 + 1 * 20 + 236 * 24 + 3 * 8 + 1338 * 32 + 130 * 120 + 272 * 216 + 422 * 16 + 11 * 824 + 1247 * 40 + 3 * 64,
	B_SYS_LISTEN: 1 * 56 + 1 * 136 + 1 * 75776 + 2 * 4120,
	B_SYS_LSEEK: 1 * 20 + 5 * 48 + 103 * 32 + 1 * 24 + 1 * 72 + 3 * 64 + 2 * 16 + 2 * 216 + 6 * 40 + 1 * 824,
	B_SYS_MKDIR: 3 * 64 + 3068 * 48 + 3 * 536 + 244 * 216 + 753 * 16 + 11 * 824 + 1190 * 40 + 177 * 120 + 3 * 1 + 1 * 4096 + 1 * 20 + 1298 * 32 + 195 * 24 + 1 * 2 + 1309 * 14 + 3 * 8,
	B_SYS_MKNOD: 9 * 824 + 1011 * 32 + 109 * 24 + 295 * 16 + 1376 * 48 + 3 * 8 + 3 * 1 + 3 * 64 + 659 * 40 + 3 * 536 + 137 * 216 + 561 * 14 + 95 * 120 + 1 * 4096 + 1 * 20,
	B_SYS_MMAP: 1 * 216 + 1 * 80 + 1 * 144 + 2 * 56 + 1 * 24 + 2 * 40 + 1 * 48 + 2 * 112,
	B_SYS_MUNMAP: 1 * 24 + 1 * 112 + 1 * 80 + 2 * 56 + 1 * 144,
	B_SYS_NANOSLEEP: 1 * 20 + 52 * 16 + 4 * 824 + 317 * 40 + 455 * 32 + 52 * 24 + 1 * 4096 + 1 * 8 + 1 * 1 + 125 * 48 + 68 * 216 + 44 * 120 + 3 * 64,
	B_SYS_OPEN: 1 * 20 + 95 * 120 + 110 * 24 + 659 * 40 + 1 * 4096 + 3 * 1 + 3 * 64 + 1377 * 48 + 137 * 216 + 295 * 16 + 9 * 824 + 3 * 8 + 1 * 4120 + 1011 * 32 + 3 * 536 + 561 * 14,
	B_SYS_PAUSE: 0,
	B_SYS_PIPE2: 56 * 24 + 317 * 40 + 455 * 32 + 68 * 216 + 52 * 16 + 2 * 56 + 2 * 4120 + 1 * 200 + 44 * 120 + 4 * 824 + 1 * 1 + 3 * 64 + 125 * 48 + 1 * 4096 + 1 * 8 + 1 * 20,
	B_SYS_POLL: (1024) * 240 + (512) * 32 + 2 * 824 + 22 * 120 + 34 * 216 + 1 * 8 + 1 * 20 + 229 * 32 + 1 * 1 + 26 * 16 + 1 * 4120 + 159 * 40 + 63 * 48 + 1 * 4096 + 27 * 24 + 3 * 64,
	B_SYS_PREAD: 238 * 40 + 33 * 120 + 3 * 824 + 344 * 32 + 1 * 112 + 1 * 20 + 3 * 64 + 94 * 48 + 51 * 216 + 1 * 8 + 1 * 1 + 39 * 24 + 39 * 16 + 1 * 4096,
	B_SYS_PROF: 1 * 64 + 64 * 1048 + 2 * 536 + 64 * 16,
	B_SYS_PWRITE: 246 * 40 + 3 * 824 + 35 * 120 + 1 * 4096 + 1 * 1 + 40 * 24 + 40 * 16 + 3 * 64 + 1 * 20 + 345 * 32 + 52 * 216 + 1 * 8 + 97 * 48 + 1 * 96,
	B_SYS_READ: 65 * 24 + 5 * 824 + 55 * 120 + 1 * 4120 + 570 * 32 + 85 * 216 + 156 * 48 + 396 * 40 + 1 * 8 + 65 * 16 + 1 * 10 + 4 * 1048 + 1 * 240 + 1 * 4096 + 1 * 1 + 3 * 64 + 1 * 20,
	B_SYS_READV: 1 * 4096 + 1 * 1 + 713 * 40 + 1 * 4120 + 99 * 120 + 1 * 240 + 4 * 1048 + 9 * 824 + 1 * 8 + 3 * 64 + 1021 * 32 + 117 * 16 + 1 * 10 + 1 * 184 + 280 * 48 + 117 * 24 + 153 * 216 + 1 * 20,
	B_SYS_REBOOT: 0,
	B_SYS_RECVFROM: 1 * 4120 + 1 * 8 + 1023 * 32 + 280 * 48 + 9 * 824 + 1 * 1 + 1 * 20 + 117 * 24 + 118 * 16 + 2 * 536 + 153 * 216 + 712 * 40 + 1 * 4096 + 99 * 120 + 3 * 64,
	B_SYS_RECVMSG: 838 * 48 + 352 * 16 + 27 * 824 + 1 * 1 + 1 * 184 + 459 * 216 + 297 * 120 + 2 * 536 + 1 * 8 + 351 * 24 + 3057 * 32 + 2135 * 40 + 1 * 4096 + 1 * 20 + 1 * 4120 + 3 * 64,
	B_SYS_RENAME: 28 * 824 + 983 * 216 + 864 * 24 + 6 * 536 + 4538 * 40 + 3666 * 32 + 469 * 120 + 3 * 2 + 7 * 8 + 4 * 56 + 1803 * 16 + 1 * 4096 + 3 * 1 + 3 * 64 + 1 * 20 + 3553 * 14 + 8970 * 48,
	B_SYS_SENDMSG: 2909 * 32 + 1 * 280 + 2262 * 40 + 3 * 64 + 404 * 24 + 1 * 20 + 1296 * 48 + 187 * 14 + 495 * 216 + 1 * 72 + 3 * 8 + 1 * 4096 + 403 * 16 + 267 * 120 + 1 * 88 + 25 * 824 + 1 * 184 + 3 * 1,
	B_SYS_SENDTO: 918 * 40 + 988 * 32 + 182 * 16 + 80 * 120 + 1 * 72 + 1 * 280 + 206 * 216 + 3 * 8 + 1 * 4096 + 1 * 20 + 8 * 824 + 187 * 14 + 3 * 1 + 3 * 64 + 183 * 24 + 769 * 48,
	B_SYS_SETRLIMIT: 2 * 824 + 159 * 40 + 34 * 216 + 26 * 16 + 1 * 4096 + 1 * 8 + 1 * 1 + 3 * 64 + 1 * 20 + 229 * 32 + 63 * 48 + 26 * 24 + 22 * 120,
	B_SYS_SETSOCKOPT: 159 * 40 + 26 * 16 + 1 * 4096 + 1 * 1 + 3 * 64 + 1 * 20 + 63 * 48 + 22 * 120 + 2 * 824 + 230 * 32 + 34 * 216 + 26 * 24 + 1 * 8,
	B_SYS_SHUTDOWN: 2 * 56 + 1 * 144 + 1 * 24,
	B_SYS_SIGACTION: 0,
	B_SYS_SOCKET: 1 * 16 + 1 * 608 + 2 * 24 + 1 * 144 + 2 * 56 + 1 * 4120,
	B_SYS_SOCKETPAIR: 2 * 4120 + 455 * 32 + 1 * 8 + 125 * 48 + 4 * 824 + 2 * 72 + 58 * 24 + 2 * 200 + 44 * 120 + 317 * 40 + 52 * 16 + 4 * 56 + 68 * 216 + 1 * 4096 + 1 * 1 + 3 * 64 + 1 * 20,
	B_SYS_STAT: 3 * 8 + 3 * 1 + 1 * 72 + 58 * 120 + 1 * 4096 + 707 * 48 + 760 * 32 + 6 * 824 + 187 * 14 + 3 * 536 + 172 * 216 + 157 * 24 + 3 * 64 + 156 * 16 + 760 * 40 + 1 * 20,
	B_SYS_SYNC: 3 * 16,
	B_SYS_THREXIT: 2 * 24 + 1 * 8 + 1 * 144 + 2 * 56,
	B_SYS_TRUNCATE: 1124 * 32 + 3 * 8 + 3 * 1 + 3 * 64 + 154 * 216 + 123 * 24 + 1408 * 48 + 308 * 16 + 1 * 20 + 740 * 40 + 1 * 4096 + 107 * 120 + 3 * 536 + 10 * 824 + 561 * 14,
	B_SYS_UNLINK: 1082 * 40 + 1211 * 32 + 3 * 8 + 209 * 24 + 106 * 120 + 1 * 20 + 2322 * 48 + 237 * 216 + 3 * 1 + 1 * 4096 + 3 * 64 + 935 * 14 + 3 * 536 + 211 * 16 + 10 * 824,
	B_SYS_WAIT4: 1 * 20 + 3 * 824 + 33 * 120 + 1 * 8 + 95 * 48 + 39 * 16 + 3 * 64 + 39 * 24 + 238 * 40 + 342 * 32 + 1 * 56 + 1 * 4096 + 51 * 216 + 1 * 1,
	B_SYS_WRITE: 457 * 32 + 1 * 20 + 52 * 16 + 4 * 824 + 126 * 48 + 1 * 4096 + 1 * 8 + 53 * 24 + 69 * 216 + 1 * 80 + 3 * 64 + 318 * 40 + 44 * 120 + 1 * 4120 + 1 * 1,
	B_SYS_WRITEV: 3 * 64 + 104 * 16 + 105 * 24 + 1 * 80 + 1 * 4120 + 1 * 4096 + 1 * 1 + 250 * 48 + 137 * 216 + 88 * 120 + 1 * 20 + 1 * 184 + 8 * 824 + 1 * 8 + 908 * 32 + 635 * 40,
	B_TCPFOPS_T_READ: 3 * 64 + 125 * 48 + 52 * 16 + 68 * 216 + 1 * 1 + 44 * 120 + 1 * 4096 + 1 * 8 + 1 * 20 + 52 * 24 + 456 * 32 + 317 * 40 + 4 * 824,
	B_TCPFOPS_T_WRITE: 52 * 16 + 4 * 824 + 44 * 120 + 456 * 32 + 68 * 216 + 52 * 24 + 1 * 4096 + 1 * 8 + 317 * 40 + 125 * 48 + 1 * 20 + 1 * 1 + 3 * 64,
	B_TCPTIMERS_T__TCPTIMERS_DAEMON: 3 * 8000 + 2 * 24 + 1 * 144 + 2 * 56,
	B_USERBUF_T__TX: 116 * 32 + 1 * 4096 + 1 * 8 + 1 * 824 + 11 * 120 + 13 * 16 + 32 * 48 + 17 * 216 + 1 * 1 + 3 * 64 + 1 * 20 + 80 * 40 + 13 * 24,
	B_USERIOVEC_T_IOV_INIT: 1 * 8 + 3 * 64 + 1 * 20 + 52 * 24 + 52 * 16 + 68 * 216 + 44 * 120 + 1 * 1 + 4 * 824 + 1 * 184 + 455 * 32 + 317 * 40 + 125 * 48 + 1 * 4096,
	B_USERIOVEC_T__TX: 159 * 40 + 26 * 16 + 230 * 32 + 22 * 120 + 34 * 216 + 63 * 48 + 26 * 24 + 2 * 824 + 1 * 4096 + 1 * 8 + 1 * 1 + 3 * 64 + 1 * 20,
}
