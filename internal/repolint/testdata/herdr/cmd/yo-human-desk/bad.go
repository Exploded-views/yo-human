package main

import "net"

func main() { _, _ = net.Dial("unix", "/srv/x.sock") }
