// Command deferred is movecheck's fixture of stages that break the rules.
package main

func main() {
	a := &app{}
	a.open()
	a.check()
	a.guard()
}
