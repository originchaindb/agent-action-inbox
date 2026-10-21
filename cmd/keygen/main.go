// Generate a random app access key locally. Do not paste this output into chat or commit it.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintln(os.Stderr, "secure random source unavailable")
		os.Exit(1)
	}
	fmt.Println(hex.EncodeToString(b))
}
