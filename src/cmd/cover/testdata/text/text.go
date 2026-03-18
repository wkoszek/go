package text

import "fmt"

// This file is tested by text_test.go.
// The comments below are markers for extracting the annotated source
// from the text output.

// This is a regression test for incorrect sorting of boundaries
// that coincide, specifically for empty select clauses.
// START f
func f() {
	ch := make(chan int)
	select {
	case <-ch:
	default:
	}
}

// END f

// https://golang.org/issue/25767
// START g
func g() {
	if false {
		fmt.Printf("Hello")
	}
}

// END g
