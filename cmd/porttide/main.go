package main

import (
	"flag"
	"fmt"
	"github.com/irtez/PortTide/internal/app"
	"os"
)

func main() {
	var o app.Options
	var noBrowser, version bool
	flag.StringVar(&o.DataDir, "data-dir", "", "private data directory")
	flag.StringVar(&o.Listen, "listen", "127.0.0.1:17790", "loopback web interface address")
	flag.StringVar(&o.Core, "core", "", "path to xray executable")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the browser")
	flag.BoolVar(&version, "version", false, "print version and exit")
	flag.Parse()
	if version {
		fmt.Println(app.Version)
		return
	}
	o.Open = !noBrowser
	if e := app.Run(o); e != nil {
		fmt.Fprintln(os.Stderr, "PortTide:", e)
		os.Exit(1)
	}
}
