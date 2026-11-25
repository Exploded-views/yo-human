package web

import n "net"

func f() { _, _ = n.DialTimeout("unix", "/srv/x.sock", 0) }
