package web

import "x/bus"

func f(p *bus.Principal) { p.Kind = "passkey" }
