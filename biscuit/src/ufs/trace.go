package ufs

import "os"
import "encoding/json"
import "fmt"

import "fs"
import "mem"

//
//  trace file of writes and syncs
//

type tracef_t struct {
	file *os.File
	enc  *json.Encoder
}

type record_t struct {
	Cmd     string
	BlkNo   int
	BlkData []byte
}

type trace_t []record_t
type order_t []int
type orders_t []order_t

func mkTrace() *tracef_t {
	t := &tracef_t{}
	f, uerr := os.Create("trace.json")
	if uerr != nil {
		panic(uerr)
	}
	t.file = f
	t.enc = json.NewEncoder(f)
	return t
}

func readTrace(p string) trace_t {
	res := make([]record_t, 0)
	f, uerr := os.Open("trace.json")
	if uerr != nil {
		panic(uerr)
	}
	dec := json.NewDecoder(f)
	for {
		var r record_t
		if err := dec.Decode(&r); err != nil {
			break
		}
		res = append(res, r)

	}
	f.Close()
	return res
}

func (trace trace_t) printTrace(start int, end int) {
	fmt.Printf("trace (%d,%d):\n", start, end)
	for i, r := range trace {
		if i >= start && i < end {
			fmt.Printf("  %d: %v %v\n", i, r.Cmd, r.BlkNo)
			//for _, b := range r.BlkData {
			//	fmt.Printf("0x%x", b)
			//}
			//fmt.Printf("\n")
		}
	}
}

func (trace trace_t) findSync(index int) int {
	for i := index; i < len(trace); i++ {
		if trace[i].Cmd == "sync" {
			return i
		}
	}
	return -1
}

func (r *record_t) copyRecord() record_t {
	c := record_t{}
	c.BlkNo = r.BlkNo
	c.Cmd = r.Cmd
	c.BlkData = make([]byte, len(r.BlkData))
	copy(c.BlkData, r.BlkData)
	return c
}

func (trace trace_t) copyTrace(start int, end int) trace_t {
	sub := make([]record_t, end-start)
	for i, _ := range sub {
		sub[i] = trace[start+i].copyRecord()
	}
	return sub
}

func (trace trace_t) permTrace(index int, o order_t) trace_t {
	new := trace.copyTrace(0, index+len(o))
	for i, j := range o {
		new[index+i] = trace[index+j]

	}
	return new
}

func (t *tracef_t) write(n int, v *mem.Bytepg_t) {
	r := record_t{}
	r.BlkNo = n
	r.Cmd = "write"
	r.BlkData = make([]byte, fs.BSIZE)
	for i, _ := range v {
		r.BlkData[i] = byte(v[i])
	}
	if err := t.enc.Encode(&r); err != nil {
		panic(err)
	}
}

func (t *tracef_t) sync() {
	r := record_t{}
	r.BlkNo = 0
	r.Cmd = "sync"
	if err := t.enc.Encode(&r); err != nil {
		panic(err)
	}
}

func (t *tracef_t) close() {
	t.file.Sync()
	t.file.Close()
}
