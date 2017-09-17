package main

import "net/http"

func main() {
	fs := http.FileServer(http.Dir("files"))
	http.Handle("/", fs)

	http.ListenAndServe(":8081", nil)
}
