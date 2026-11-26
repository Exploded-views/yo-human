package cli

import "net"

// The CLI side is out of scope: its channels may do what they like.
func f() { _, _ = net.Dial("unix", "/srv/x.sock") }
