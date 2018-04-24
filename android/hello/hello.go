// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hello is a trivial package for gomobile bind example.
package hello

import (
	"fmt"
)

type Message struct {
	hash string
	path string
}

func Hashlist(channel string) []Message {
	messages := make([]Message, 4)
	for i := 0; i < 4; i++ {
		var messages Message
		messages.hash = "bar"
		messages.path = "foo"
	}
	return messages
}

func Hashtree(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}
