package main

import (
	"log"
)

func main() {
	fs := FileServer(Dir("/files"))
	Handle("/", fs)

	log.Println("Listening...")
	ListenAndServe(":8080")
}
