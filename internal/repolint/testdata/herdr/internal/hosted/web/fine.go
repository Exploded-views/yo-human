package web

import "net"

// herdr appears in this comment only, and a tcp dial is no session wake.
func fine() { _, _ = net.Dial("tcp", "127.0.0.1:1") }
