package web

import (
	"context"
	"net"
)

func f() { var d net.Dialer; _, _ = d.DialContext(context.Background(), "unixpacket", "/srv/x.sock") }
