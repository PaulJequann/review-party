package main

import (
	"fmt"
	"io"
	"sync"
)

func newRunWarningSink(stderr io.Writer) func(string) {
	var mutex sync.Mutex
	return func(line string) {
		mutex.Lock()
		defer mutex.Unlock()
		if _, err := fmt.Fprintln(stderr, line); err != nil {
			return
		}
	}
}
