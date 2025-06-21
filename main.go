package main

import "snapchain-monitor/cmd"

var SCMON_VERSION string

func main() {
	cmd.Version = SCMON_VERSION
	cmd.Execute()
}
