// Package main provides a simple CLI tool to list files in a UDF image.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"golift.io/udf"
)

func printDir(spaces string, files []udf.File) {
	for _, file := range files {
		fmt.Printf("%s %-10d %s %-20s %v\n", file.Mode().String(), file.Size(), spaces, file.Name(), file.ModTime())

		if file.IsDir() {
			children, err := file.ReadDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading dir %s: %v\n", file.Name(), err)
				continue
			}

			printDir(spaces+"   ", children)
		}
	}
}

func run() error {
	flag.Parse()

	if flag.NArg() == 0 {
		return errors.New("usage: isoinfo <file.iso>") //nolint:err113
	}

	rdr, err := os.Open(flag.Arg(0))
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	defer func() { _ = rdr.Close() }()

	image, err := udf.NewUdfFromReader(rdr)
	if err != nil {
		return fmt.Errorf("udf: %w", err)
	}

	files, err := image.ReadDir(nil)
	if err != nil {
		return fmt.Errorf("readdir: %w", err)
	}

	printDir("", files)

	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
