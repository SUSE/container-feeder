package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	const defaultImageLocation string = "/usr/share/suse-docker-images/native"

	var dir = flag.String("dir", defaultImageLocation, "directory containing the images to import")
	flag.Parse()

	if *dir == "" {
		fmt.Println("missing mandatory `--dir` value")
		os.Exit(1)
	}

	feeder, err := NewFeeder()
	if err != nil {
		log.Printf("Something went wrong while initializing the image feeder: %v\n", err)
		os.Exit(1)
	}

	importResp, err := feeder.Import(*dir)
	if err != nil {
		log.Printf("Something went wrong while imporing the images: %v\n", err)
		os.Exit(1)
	}

	if len(importResp.SuccessfulImports) > 0 {
		fmt.Println("Successfully imported the following images:")
	}
	for _, image := range importResp.SuccessfulImports {
		fmt.Printf("  - %s\n", image)
	}

	if len(importResp.FailedImports) > 0 {
		fmt.Println("The following images failed to be imported:")
	}
	for _, failedImport := range importResp.FailedImports {
		fmt.Printf("  - %s with error: %v\n", failedImport.Image, failedImport.Error)
	}
}
