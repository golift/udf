# `udf`

Go Library for reading UDF (Universal Disc Format) filesystem images.

-   [GoDoc](https://pkg.go.dev/golift.io/udf)
-   Works on Linux, Windows, FreeBSD and macOS **without Cgo**.
-   Parses UDF volume structures per ECMA-167.
-   Returns errors instead of panicking.

Forked from [mogaika/udf](https://github.com/mogaika/udf) with bug fixes,
error handling, and modernization.

# Example

```golang
package main

import (
	"fmt"
	"log"
	"os"

	"golift.io/udf"
)

func main() {
	r, err := os.Open("example.iso")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	u, err := udf.NewUdfFromReader(r)
	if err != nil {
		log.Fatal(err)
	}

	files, err := u.ReadDir(nil)
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		fmt.Printf("%s %-10d %-20s %v\n", f.Mode().String(), f.Size(), f.Name(), f.ModTime())
	}
}
```
