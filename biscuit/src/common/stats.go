package common

import "reflect"
import "sync/atomic"
import "strconv"
import "strings"
import "unsafe"

type Counter_t int64
type Cycles_t int64

func (c *Counter_t) Inc() {
	n := (*int64)(unsafe.Pointer(c))
	atomic.AddInt64(n, 1)
}

func (c *Cycles_t) Add(m uint64) {
	n := (*int64)(unsafe.Pointer(c))
	atomic.AddInt64(n, int64(m))
}

func Stats2String(st interface{}) string {
	v := reflect.ValueOf(st)
	s := ""
	for i := 0; i < v.NumField(); i++ {
		t := v.Field(i).Type().String()
		if strings.HasSuffix(t, "Counter_t") {
			n := v.Field(i).Interface().(Counter_t)
			s += "\n\t#" + v.Type().Field(i).Name + ": " + strconv.FormatInt(int64(n), 10)
		}
		if strings.HasSuffix(t, "Cycles_t") {
			n := v.Field(i).Interface().(Cycles_t)
			s += "\n\t#" + v.Type().Field(i).Name + ": " + strconv.FormatInt(int64(n), 10)
		}

	}
	return s + "\n"
}
