package web

import . "net"

func f() { _, _ = DialTimeout("unix", "/srv/x.sock", 0) }
