package unet

import "fmt"
import "sync/atomic"
import "testing"

import "defs"
import "fdops"
import . "inet"
import "log"

const NBYTES = 1024
const VAL = 1
const VAL1 = 2

func doClnt(clnt fdops.Fdops_i, sa []uint8, t *testing.T) {
	p, ip := unSaddr(sa)
	log.Printf("accepted clnt (%s,%d)\n", Ip2str(ip), p)
	data := make([]uint8, NBYTES)
	ub := mkUbuf(data)
	n, err := clnt.Read(ub)
	if err != 0 {
		t.Fatalf("read")
	}
	if n != NBYTES {
		t.Fatalf("short read")
	}
	for i, v := range data {
		if v != VAL {
			t.Fatalf("read wrong data %d %d\n", i, v)
		}
	}
	ub = mkData(VAL1, NBYTES)
	n, err = clnt.Write(ub)
	if err != 0 {
		t.Fatalf("write")
	}
	if n != NBYTES {
		t.Fatalf("short write")
	}
	clnt.Close()
}

func mkServerConn(port int, t *testing.T) fdops.Fdops_i {
	rcv := mkTcpfops()
	sa := mkSaddr(port, defs.INADDR_ANY)
	err := rcv.Bind(sa)
	if err != 0 {
		t.Fatalf("Bind %d\n", err)
	}
	conn, err := rcv.Listen(10)
	if err != 0 {
		t.Fatalf("Listen %d\n", err)
	}
	return conn
}

func server(conn fdops.Fdops_i, t *testing.T, done *int32) {
	for atomic.LoadInt32(done) != 1 {
		sa := make([]uint8, 8)
		ub := mkUbuf(sa)
		clnt, _, err := conn.Accept(ub)
		if err != 0 {
			t.Fatalf("accept")
		}
		go doClnt(clnt, sa, t)
	}
}

func client(port int, t *testing.T) {
	log.Printf("client")

	snd := mkTcpfops()
	sa := mkSaddr(port, 0x7f000001)
	err := snd.Connect(sa)
	if err != 0 {
		t.Fatalf("connect %d", err)
	}

	ub := mkData(VAL, NBYTES)
	n, err := snd.Write(ub)
	if err != 0 {
		t.Fatalf("write")
	}
	if n != NBYTES {
		t.Fatalf("short write")
	}

	data := make([]uint8, NBYTES)
	ub = mkUbuf(data)
	n, err = snd.Read(ub)
	if err != 0 {
		t.Fatalf("read")
	}
	if n != NBYTES {
		t.Fatalf("short read")
	}

	for i, v := range data {
		if v != VAL1 {
			t.Fatalf("read wrong data %d %d\n", i, v)
		}
	}

	err = snd.Close()
	if err != 0 {
		t.Fatalf("close %d", err)
	}
}

func TestSimple(t *testing.T) {
	net_init()

	fmt.Printf("TestConnect\n")

	conn := mkServerConn(1090, t)
	done := int32(0)
	go server(conn, t, &done)

	client(1090, t)

	atomic.StoreInt32(&done, 1)

	fmt.Printf("TestConnect Done\n")
}

const NCLIENT = 5

func TestClients(t *testing.T) {
	net_init()

	fmt.Printf("TestConnect\n")

	conn := mkServerConn(1090, t)
	ch := make(chan bool, NCLIENT)
	done := int32(0)
	go server(conn, t, &done)

	for i := 0; i < NCLIENT; i++ {
		go func(clnt int, t *testing.T) {
			defer func() {
				ch <- true
			}()
			client(1090, t)
		}(i, t)
	}
	// wait for clients to be done
	for i := 0; i < NCLIENT; i++ {
		<-ch
	}
	atomic.StoreInt32(&done, 1)
	fmt.Printf("TestClients Done\n")
}
