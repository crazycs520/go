package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime/trace"
	"sync"

	"runtime"
	"time"
)

func init() {
	runtime.EnableStats()
}

func main() {
	file, err := os.OpenFile("trace.out", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	trace.Start(file)
	defer func() {
		trace.Stop()
		file.Close()

	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go testGoroutine(&wg)
	wg.Wait()
}

func testGoroutine(wg *sync.WaitGroup) {
	defer wg.Done()
	sum(10e8)
	time.Sleep(time.Millisecond * 100)
	writeFile()
	curl()
	blockChannel()
	stats := runtime.GetGStats()
	printGStats(stats)
}

func sum(n int) {
	sum := 0
	for i := 0; i < n; i++ {
		sum += i
	}
}

func writeFile() {
	var data = make([]byte, 1*1024*1024)
	for i := 0; i < len(data); i++ {
		data[i] = 'a' + byte(i%26)
	}
	wf, err := os.OpenFile("write.txt", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, err := wf.Write(data)
		if err != nil {
			log.Fatal(err)
		}
	}
	wf.Close()
}

func curl() {
	response, err := http.Get("http://www.bing.com")
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	ioutil.ReadAll(response.Body)
}

func blockChannel() {
	ch := make(chan int)
	go func() {
		time.Sleep(time.Millisecond * 100)
		<-ch
	}()
	ch <- 0
}

func printGStats(s *runtime.GStats) {
	fmt.Printf("total: %s, exec: %s, network_wait: %s, sync_block: %s, block_syscall: %s, scheduler_wait: %s, gc_sweep: %s\n",
		time.Duration(s.TotalTime()).String(),
		time.Duration(s.ExecTime()).String(),
		time.Duration(s.IOTime()).String(),
		time.Duration(s.BlockTime()).String(),
		time.Duration(s.SyscallTime()).String(),
		time.Duration(s.SchedWaitTime()).String(),
		time.Duration(s.SweepTime()).String())
}
