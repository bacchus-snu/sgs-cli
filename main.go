package main

import (
	"flag"
	"fmt"
)

func main() {
	var name string
	flag.StringVar(&name, "name", "everyone", "The greeting object.")
	flag.Parse()
	fmt.Printf("Hello, %s!\n", name)
}
