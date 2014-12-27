package main

import "fmt"

func main() {
        fmt.Print("hello world ")
	m := make(map[int]string)
	m[1] = "help"
	m[2] = "i"
	m[4] = "don't"
	m[100] = "know"
	m[101] = "go"

	for k, v := range m {
		fmt.Printf("%v %v ", k, v)
	}
}
