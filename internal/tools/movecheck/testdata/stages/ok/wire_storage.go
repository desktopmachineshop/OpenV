package main

// storage opens the store. The goroutine's defer and return are its own.
func (a *app) storage() {
	a.db = a.name + ".db"
	go func() {
		defer println("background done")
		for i := 0; i < 3; i++ {
			if i == 2 {
				return
			}
		}
	}()
	a.ready = true
}

func (a *app) serve() { println("serving", a.db) }
