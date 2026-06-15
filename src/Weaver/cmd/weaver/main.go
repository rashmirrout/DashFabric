package main

import (
	"flag"
	"log"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	log.Printf("Weaver Gateway starting with config: %s\n", *configPath)
	log.Println("TODO: Implement gateway initialization")
}
