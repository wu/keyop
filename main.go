// main package: entrypoint for keyop CLI application.
package main

import (
	"keyop/cmd"
	_ "time/tzdata" // embed timezone database so LoadLocation works on minimal servers
)

func main() {
	cmd.Execute()
}
