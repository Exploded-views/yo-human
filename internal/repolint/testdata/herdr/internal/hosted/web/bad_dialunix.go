package web

import "net"

func f() { _, _ = net.DialUnix("unix", nil, nil) }
