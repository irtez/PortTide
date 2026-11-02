package main

import (
	"flag"
	"fmt"
	"github.com/irtez/PortTide/internal/app"
	"os"
)

func main() {
	var o app.Options
	var version bool
	flag.StringVar(&o.DataDir, "data-dir", "", "private data directory")
	flag.StringVar(&o.Listen, "listen", "127.0.0.1:17790", "loopback web interface address")
	flag.StringVar(&o.Core, "core", "", "path to xray executable")
	flag.BoolVar(&o.NoBrowser, "no-browser", false, "do not open the browser")
	flag.BoolVar(&version, "version", false, "print version and exit")
	flag.BoolVar(&o.Open, "open", false, "explicitly open the existing or new panel")
	flag.BoolVar(&o.Background, "background", false, "launch at logon using background preference")
	flag.BoolVar(&o.Resume, "resume", false, "resume the pre-update running state without opening a browser")
	flag.StringVar(&o.Command, "command", "", "instance command: prepare-update, shutdown, open")
	flag.Parse()
	if version {
		fmt.Println(app.Version)
		return
	}
	if e := app.Run(o); e != nil {
		if o.Command == "" {
			app.StartupError(e, o.Background || o.NoBrowser || o.Resume)
		}
		fmt.Fprintln(os.Stderr, "PortTide:", e)
		os.Exit(1)
	}
}
