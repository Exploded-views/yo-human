package nudge

import "net"

func f() { _, _ = net.Dial("unix", "/srv/x.sock") }
