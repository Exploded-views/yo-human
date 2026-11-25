package web

import "net"

var a = &net.UnixAddr{Name: "/srv/x.sock", Net: "unix"}
