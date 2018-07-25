package unet

import "fmt"
import "testing"

import "defs"
import "fdops"
import . "inet"
import "log"

func doClnt(conn fdops.Fdops_i, t *testing.T) {
	log.Printf("clnt: wait for accept\n")

	sa := make([]uint8, 8)
	ub := mkUbuf(sa)
	clnt, _, err := conn.Accept(ub)
	if err != 0 {
		t.Fatalf("accept")
	}
	if err != 0 {
		t.Fatalf("accept sa")
	}
	p, ip := unSaddr(sa)
	log.Printf("accepted clnt (%s,%d)\n", Ip2str(ip), p)
	clnt.Close()
}

func serverinit(port int, t *testing.T) fdops.Fdops_i {
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

func server(conn fdops.Fdops_i, t *testing.T) {
	for true {
		doClnt(conn, t)
	}
}

func client(port int, t *testing.T) {
	snd := mkTcpfops()
	sa := mkSaddr(port, 0x7f000001)
	err := snd.Connect(sa)
	if err != 0 {
		t.Fatalf("connect %d", err)
	}
	err = snd.Close()
	if err != 0 {
		t.Fatalf("close %d", err)
	}
}

func TestConnect(t *testing.T) {
	net_init()

	fmt.Printf("TestConnect\n")

	conn := serverinit(1090, t)
	go server(conn, t)

	client(1090, t)

	fmt.Printf("TestConnect Done\n")
}
