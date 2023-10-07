package main

import (
	"sync"
	"time"
)

var vg sync.WaitGroup

func main() {
	vg.Add(1)
	ch := make(chan int)
	defer close(ch)

	go func(ch chan int) {
		time.Sleep(time.Second)
		<-ch
	}(ch)

	ch <- 1
	vg.Wait()
}
