// Command ok is movecheck's fixture of a main() that only sequences stages.
package main

func main() {
	a := &app{}
	stop := a.config()
	defer stop()
	a.storage()
	if a.ready {
		a.serve()
	}
	a.done = true
}
