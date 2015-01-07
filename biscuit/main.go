package main

import "fmt"
import "math/rand"

func main() {
	ch := make(chan int)
	var foo int

	bar := func(n int) {
		fmt.Printf(" [thread %v] ", n)
		foo = n
		ch <- rand.Intn(100)
	}

	go bar(1)
	go bar(2)

        fmt.Print("hello world ")
	m := make(map[int]string)
	m[1] = "help"
	m[2] = "i"
	m[4] = "don't"
	m[100] = "know"
	m[101] = "go"

	for _, v := range m {
		fmt.Printf("%v ", v)
	}

	ret := <- ch
	ret = <- ch
	fmt.Printf("done %v %v", ret, foo)
}
