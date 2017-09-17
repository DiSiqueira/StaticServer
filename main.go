package main

import (
	"log"
)

func main() {
	urls := fileTable{}
	urls["/"] = NewFile("./files/teste.html", ContentTypeHTML)
	urls["/index"] = NewFile("./files/index.html", ContentTypeHTML)

	log.Println("Listening...")

	ListenAndServe(8080, NewFileTableServer(urls))
}
