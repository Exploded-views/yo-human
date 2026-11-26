package web

import "net"

func f() { _, _ = net.DialTimeout("unixgram", "/srv/x.sock", 0) }
