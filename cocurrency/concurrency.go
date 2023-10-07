package main

import (
	"fmt"
	"sync"
)

var vg sync.WaitGroup

func main() {
	c := make(chan int)
	vg.Add(2)
	go func1(c)
	go func2(c)
	vg.Wait()
}

func func1(c chan int) {
	for i := 0; i < 10; i++ {
		fmt.Println("put values")
		c <- i
	}
	vg.Done()
}

func func2(c chan int) {
	for i := 0; i < 10; i++ {
		fmt.Println("get values", <-c)
	}
	vg.Done()
}
